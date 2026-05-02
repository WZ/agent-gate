//go:build windows

package launcher

import (
	"io"
	"syscall"
)

// ttyAwareSysProcAttr on Windows is a no-op: Windows doesn't have the
// SIGTTIN/foreground-process-group mechanism. Returning nil leaves the
// child to inherit stdio normally; CreationFlags etc. are set elsewhere.
func ttyAwareSysProcAttr(stdin io.Reader) *syscall.SysProcAttr {
	_ = stdin
	return nil
}
