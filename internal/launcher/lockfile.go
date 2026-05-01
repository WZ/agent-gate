package launcher

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type lockfile struct {
	path string
	f    *os.File
}

// acquireLockfile creates path with O_EXCL. If it already exists, reads the
// PID inside and reports who has the lock. Caller MUST defer release().
func acquireLockfile(path string) (*lockfile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
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
