package launcher

import (
	"context"
	"errors"
	"os/exec"
)

// waitCmd waits for cmd to exit, returning its exit code. If ctx is cancelled
// first, the caller is responsible for killing the process; waitCmd just
// reports back what the OS observed.
func waitCmd(ctx context.Context, cmd *exec.Cmd) (int, error) {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-ctx.Done():
		return 130, ctx.Err()
	case err := <-done:
		if err == nil {
			return 0, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
}
