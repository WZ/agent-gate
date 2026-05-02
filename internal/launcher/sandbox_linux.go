//go:build linux

package launcher

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

type childHandle struct {
	cmd *exec.Cmd
}

func (h *childHandle) wait(ctx context.Context) (int, error) {
	if h == nil || h.cmd == nil {
		return 1, errors.New("launcher: nil childHandle")
	}
	return waitCmd(ctx, h.cmd)
}

func (h *childHandle) kill() error {
	if h == nil || h.cmd == nil || h.cmd.Process == nil {
		return nil
	}
	return h.cmd.Process.Kill()
}

func airtightFeasible() (bool, string) {
	// Probe by re-execing ourselves with the same Cloneflags + ExtraFiles
	// shape that spawnAirtight uses, plus an extra "--probe" arg. The probed
	// process tries: (1) bring lo up, (2) bind 127.0.0.1:0, (3) report success
	// via FD 3. If any step fails we fall back to permissive (or abort if
	// --airtight-fail). This catches Ubuntu 24's apparmor_restrict_unprivileged_userns
	// which permits unshare but blocks bind() inside the namespace.
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return false, fmt.Sprintf("socketpair: %v", err)
	}
	supEnd := os.NewFile(uintptr(pair[0]), "probe-sup")
	helperEnd := os.NewFile(uintptr(pair[1]), "probe-helper")
	defer supEnd.Close()

	self, err := os.Executable()
	if err != nil {
		helperEnd.Close()
		return false, fmt.Sprintf("os.Executable: %v", err)
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		helperEnd.Close()
		return false, fmt.Sprintf("EvalSymlinks: %v", err)
	}

	cmd := exec.Command(self, "__netns-helper", "0") // port 0 = "probe mode"
	cmd.ExtraFiles = []*os.File{helperEnd}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:                 unix.CLONE_NEWUSER | unix.CLONE_NEWNET,
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
		Pdeathsig:                  unix.SIGKILL,
		GidMappingsEnableSetgroups: false,
	}
	if err := cmd.Start(); err != nil {
		helperEnd.Close()
		return false, fmt.Sprintf("user+net namespace unavailable: %v (try `sudo sysctl -w kernel.unprivileged_userns_clone=1`; on Ubuntu 24 also `sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0`)", err)
	}
	helperEnd.Close()

	// Helper does port=0 → bind to ephemeral port; if that fails it returns
	// non-zero. We just wait for it to finish and report.
	if err := cmd.Wait(); err != nil {
		return false, fmt.Sprintf("airtight probe failed inside namespace (likely AppArmor unprivileged-userns restriction): %v", err)
	}
	return true, ""
}

// spawnAirtight forks a helper into a new user+net namespace, receives the
// listener FD over a control socket, hands it to the proxy goroutine via
// the exported channel on Options, then sends an EXEC frame so the helper
// exec's the target.
func spawnAirtight(ctx context.Context, opts Options, env []string) (*childHandle, error) {
	port, err := portFromAddr(opts.ProxyAddr)
	if err != nil {
		return nil, err
	}

	// Create socketpair (sup, helper).
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("socketpair: %w", err)
	}
	supEnd := os.NewFile(uintptr(pair[0]), "ctl-sup")
	helperEnd := os.NewFile(uintptr(pair[1]), "ctl-helper")

	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return nil, err
	}

	helper := exec.CommandContext(ctx, self, "__netns-helper", fmt.Sprintf("%d", port))
	helper.Stdin = opts.Stdin
	helper.Stdout = opts.Stdout
	helper.Stderr = opts.Stderr
	helper.ExtraFiles = []*os.File{helperEnd}
	helper.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:                 unix.CLONE_NEWUSER | unix.CLONE_NEWNET,
		UidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getuid(), Size: 1}},
		GidMappings:                []syscall.SysProcIDMap{{ContainerID: 0, HostID: os.Getgid(), Size: 1}},
		Pdeathsig:                  unix.SIGKILL,
		GidMappingsEnableSetgroups: false,
	}
	if err := helper.Start(); err != nil {
		supEnd.Close()
		helperEnd.Close()
		return nil, fmt.Errorf("helper start: %w", err)
	}
	// We no longer need our copy of the helper's end of the pair.
	helperEnd.Close()

	// Recv listener FD via SCM_RIGHTS on the supervisor end.
	nsFD, err := recvFD(supEnd)
	if err != nil {
		_ = helper.Process.Kill()
		_ = helper.Wait()
		supEnd.Close()
		return nil, fmt.Errorf("recvFD: %w", err)
	}
	nsFile := os.NewFile(uintptr(nsFD), "ns-listener")
	nsLn, err := net.FileListener(nsFile)
	if err != nil {
		nsFile.Close()
		_ = helper.Process.Kill()
		_ = helper.Wait()
		supEnd.Close()
		return nil, fmt.Errorf("net.FileListener: %w", err)
	}
	nsFile.Close() // FileListener dups; safe to close ours.

	// Hand the netns listener to the proxy goroutine.
	opts.nsListener <- nsLn

	// Send EXEC frame.
	if err := sendExecFrame(supEnd, opts.Cmd, execArgv(opts.Cmd, opts.Args), env); err != nil {
		_ = helper.Process.Kill()
		_ = helper.Wait()
		supEnd.Close()
		return nil, fmt.Errorf("sendExec: %w", err)
	}
	supEnd.Close() // helper has all it needs

	return &childHandle{cmd: helper}, nil
}

// recvFD reads one SCM_RIGHTS message off ctl and returns the inner FD.
func recvFD(ctl *os.File) (int, error) {
	buf := make([]byte, 16)
	oob := make([]byte, syscall.CmsgSpace(4))
	n, oobn, _, _, err := syscall.Recvmsg(int(ctl.Fd()), buf, oob, 0)
	if err != nil {
		return -1, err
	}
	_ = n
	cmsgs, err := syscall.ParseSocketControlMessage(oob[:oobn])
	if err != nil {
		return -1, err
	}
	for _, cm := range cmsgs {
		if cm.Header.Level == syscall.SOL_SOCKET && cm.Header.Type == syscall.SCM_RIGHTS {
			fds, err := syscall.ParseUnixRights(&cm)
			if err != nil {
				return -1, err
			}
			if len(fds) > 0 {
				// Mark CLOEXEC explicitly.
				_, _ = unix.FcntlInt(uintptr(fds[0]), unix.F_SETFD, unix.FD_CLOEXEC)
				return fds[0], nil
			}
		}
	}
	return -1, errors.New("no SCM_RIGHTS in message")
}

func sendExecFrame(ctl *os.File, exe string, argv, env []string) error {
	var buf []byte
	buf = append(buf, []byte("EXEC")...)
	buf = appendLenString(buf, exe)
	buf = appendU32(buf, uint32(len(argv)))
	for _, a := range argv {
		buf = appendLenString(buf, a)
	}
	buf = appendU32(buf, uint32(len(env)))
	for _, e := range env {
		buf = appendLenString(buf, e)
	}
	_, err := ctl.Write(buf)
	return err
}

func appendU32(b []byte, v uint32) []byte {
	var x [4]byte
	binary.LittleEndian.PutUint32(x[:], v)
	return append(b, x[:]...)
}

func appendLenString(b []byte, s string) []byte {
	b = appendU32(b, uint32(len(s)))
	return append(b, s...)
}

func portFromAddr(addr string) (int, error) {
	if addr == "" {
		return 8888, nil
	}
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0, err
	}
	port := 0
	for _, c := range portStr {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid port %q", portStr)
		}
		port = port*10 + int(c-'0')
	}
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port range %q", portStr)
	}
	return port, nil
}
