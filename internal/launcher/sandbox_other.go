//go:build !darwin && !linux && !windows

package launcher

import "context"

// childHandle is platform-defined; the !darwin/linux/windows build never spawns one.
type childHandle struct{}

func (h *childHandle) wait(context.Context) (int, error) { return 1, ErrUnsupported }
func (h *childHandle) kill() error                       { return nil }

func spawnAirtight(ctx context.Context, opts Options, env []string) (*childHandle, error) {
	return nil, ErrUnsupported
}

func airtightFeasible() (ok bool, reason string) {
	return false, "airtight unsupported on this OS"
}
