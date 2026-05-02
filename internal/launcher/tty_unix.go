//go:build darwin || linux

package launcher

import (
	"io"
	"os"
	"syscall"
)

// ttyAwareSysProcAttr returns SysProcAttr for spawning a child whose stdin
// may be a TTY. We always Setpgid so teardown can SIGKILL the whole pgroup;
// when stdin is a TTY we additionally set Foreground+Ctty so the child's
// pgroup becomes the TTY's foreground group — without this, the child gets
// SIGTTIN on read() and stops, which manifests as "I can't type into Claude".
func ttyAwareSysProcAttr(stdin io.Reader) *syscall.SysProcAttr {
	a := &syscall.SysProcAttr{Setpgid: true}
	f, ok := stdin.(*os.File)
	if !ok || f == nil {
		return a
	}
	fi, err := f.Stat()
	if err != nil {
		return a
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		// stdin is a pipe / regular file / something non-TTY — keep Setpgid only.
		return a
	}
	a.Foreground = true
	a.Ctty = int(f.Fd())
	return a
}
