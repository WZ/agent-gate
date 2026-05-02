package launcher

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

type lockfile struct {
	path string
	f    *os.File
}

// acquireLockfile creates path with O_EXCL. If a lockfile already exists, we
// inspect it: if the PID inside is still alive, we refuse with a friendly
// error. If the PID is gone (kill -9'd previous run, crash, reboot), we
// silently reclaim the stale lockfile and proceed.
//
// Caller MUST defer release().
func acquireLockfile(path string) (*lockfile, error) {
	f, err := tryOpenExclusive(path)
	if errors.Is(err, os.ErrExist) {
		// Lockfile present. Decide whether the holder is alive.
		pid := readLockPid(path)
		if pid > 0 && processAlive(pid) {
			return nil, lockHeldError(path)
		}
		// Stale lock — remove and retry once.
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, os.ErrNotExist) {
			return nil, fmt.Errorf("remove stale lockfile %s: %w", path, rmErr)
		}
		f, err = tryOpenExclusive(path)
	}
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, lockHeldError(path)
		}
		return nil, err
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return &lockfile{path: path, f: f}, nil
}

func tryOpenExclusive(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
}

// processAlive returns true if pid corresponds to a running process. Sends
// signal 0 (no-op delivery) and inspects errno. ESRCH = gone. EPERM = exists
// but we lack permission to signal it (treat as alive).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
		return false
	}
	// EPERM and friends — process exists, just not ours to signal.
	return true
}

func lockHeldError(path string) error {
	pid := readLockPid(path)
	if pid > 0 {
		return fmt.Errorf("another agent-gate run is active (PID %d). Stop it first, or remove %s if stale.", pid, path)
	}
	return fmt.Errorf("another agent-gate run is active. Lockfile: %s. Remove it if stale.", path)
}

func readLockPid(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return pid
}

func (l *lockfile) release() {
	if l == nil || l.f == nil {
		return
	}
	_ = l.f.Close()
	_ = os.Remove(l.path)
}
