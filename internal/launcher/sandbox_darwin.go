//go:build darwin

package launcher

import (
	"context"
	"errors"
	"os/exec"
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

func spawnAirtight(ctx context.Context, opts Options, env []string) (*childHandle, error) {
	return nil, errors.New("launcher: macOS sandbox not implemented yet")
}

func airtightFeasible() (ok bool, reason string) {
	return false, "darwin sandbox not yet implemented"
}
