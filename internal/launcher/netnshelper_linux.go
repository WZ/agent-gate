//go:build linux

package launcher

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// RunNetnsHelper is the body of `agent-gate __netns-helper <port>`. It runs
// inside CLONE_NEWUSER|CLONE_NEWNET. Steps:
//
//	(a) write UID/GID maps (preconfigured by SysProcAttr; nothing to do)
//	(b) bring up `lo`
//	(c) bind 127.0.0.1:<port>
//	(d) send listener FD back to supervisor via FD 3 (the socketpair end)
//	(e) read EXEC <argc> <argv...> from FD 3
//	(f) syscall.Exec the requested target
//
// PROBE MODE (port == 0): only steps (b)+(c) — bind to ephemeral port — then
// exit 0. No FD-passing, no EXEC frame. Used by airtightFeasible to detect
// distros (e.g., Ubuntu 24 with apparmor_restrict_unprivileged_userns=1) that
// permit unshare but block bind() inside the namespace.
func RunNetnsHelper(args []string) error {
	if len(args) < 1 {
		return errors.New("__netns-helper: missing port arg")
	}
	port, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("__netns-helper: bad port %q: %w", args[0], err)
	}

	if err := bringLoUp(); err != nil {
		return fmt.Errorf("bring lo up: %w", err)
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("bind %d: %w", port, err)
	}
	if port == 0 {
		// Probe mode — bind succeeded; that's the proof we needed. Exit 0.
		_ = ln.Close()
		return nil
	}
	tcpLn, ok := ln.(*net.TCPListener)
	if !ok {
		return errors.New("listener is not *net.TCPListener")
	}
	f, err := tcpLn.File()
	if err != nil {
		return fmt.Errorf("listener.File: %w", err)
	}
	defer f.Close()

	// FD 3 is the control socketpair end the supervisor passed in via ExtraFiles.
	ctl := os.NewFile(3, "ctl")
	if err := sendFD(ctl, int(f.Fd())); err != nil {
		return fmt.Errorf("sendFD: %w", err)
	}

	// Wait for EXEC frame from supervisor.
	exe, argv, env, err := readExecFrame(ctl)
	if err != nil {
		return fmt.Errorf("read exec frame: %w", err)
	}

	// All FDs except stdin/stdout/stderr should already be CLOEXEC; ctl was
	// inherited as ExtraFiles so it's not. Close it explicitly before exec.
	_ = ctl.Close()
	_ = ln.Close()

	return syscall.Exec(exe, argv, env)
}

// bringLoUp brings the loopback interface up inside the netns.
func bringLoUp() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var ifr struct {
		Name  [unix.IFNAMSIZ]byte
		Flags uint16
		_pad  [22]byte
	}
	copy(ifr.Name[:], "lo")
	ifr.Flags = unix.IFF_UP
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd),
		uintptr(unix.SIOCSIFFLAGS), uintptr(unsafe.Pointer(&ifr)))
	if errno != 0 {
		return fmt.Errorf("ioctl SIOCSIFFLAGS: %w", errno)
	}
	return nil
}

// sendFD passes one file descriptor over a UNIX SOCK_SEQPACKET socket using SCM_RIGHTS.
func sendFD(ctl *os.File, fd int) error {
	rights := syscall.UnixRights(fd)
	return syscall.Sendmsg(int(ctl.Fd()), []byte("FD"), rights, nil, 0)
}

// readExecFrame reads:  EXEC <argcU32><len-prefixed exe><len-prefixed args...><env-len><len-prefixed envs...>
func readExecFrame(ctl *os.File) (exe string, argv, env []string, err error) {
	header := make([]byte, 4)
	if _, err = io.ReadFull(ctl, header); err != nil {
		return
	}
	if string(header) != "EXEC" {
		err = fmt.Errorf("bad header %q", header)
		return
	}
	exe, err = readLenString(ctl)
	if err != nil {
		return
	}
	argc, err := readU32(ctl)
	if err != nil {
		return
	}
	argv = make([]string, argc)
	for i := range argv {
		argv[i], err = readLenString(ctl)
		if err != nil {
			return
		}
	}
	envc, err := readU32(ctl)
	if err != nil {
		return
	}
	env = make([]string, envc)
	for i := range env {
		env[i], err = readLenString(ctl)
		if err != nil {
			return
		}
	}
	return
}

func readU32(r io.Reader) (uint32, error) {
	var b [4]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b[:]), nil
}

func readLenString(r io.Reader) (string, error) {
	n, err := readU32(r)
	if err != nil {
		return "", err
	}
	if n > 1<<20 {
		return "", fmt.Errorf("string too long: %d", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return string(b), nil
}
