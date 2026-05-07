//go:build windows

package launcher

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/tailscale/wf"
	"golang.org/x/sys/windows"
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

// airtightFeasible reports whether airtight mode is currently usable on this
// host.
//
// v0.3.0 STATE: airtight on Windows is not yet wired. We always return false
// with a clear message; the supervisor falls back to permissive mode. Plan 4
// will land the full Job Object + WFP filter implementation, including the
// IoCompletionPort listener for descendant filtering (see Path B in the
// Task 12 spike notes).
//
// `agent-gate init` on Windows still registers the persistent WFP provider
// and sublayer — those land now (Task 14) so Plan 4's runtime path can rely
// on them already existing.
func airtightFeasible() (bool, string) {
	major, _, build := windows.RtlGetNtVersionNumbers()
	if major < 10 || (major == 10 && build < 17763) {
		return false, "Windows 10 1809 or later required"
	}
	return false, "Windows airtight is pending Plan 4 (use --mode=permissive); see README"
}

func spawnAirtight(ctx context.Context, opts Options, env []string) (*childHandle, error) {
	// Belt-and-suspenders: even if the supervisor's feasibility probe is
	// bypassed, fail loudly here with a clear message rather than half-running.
	return nil, errors.New("launcher: Windows airtight is pending Plan 4 (use --mode=permissive); see README")
}

// portFromAddr is shared with sandbox_darwin / sandbox_linux but each platform
// keeps its own copy because Go build tags make a single shared file awkward
// (the file would need its own build tag set excluding `other`).
func portFromAddr(addr string) (int, error) {
	if addr == "" {
		return 8888, nil
	}
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] != ':' {
			continue
		}
		p := 0
		for _, c := range addr[i+1:] {
			if c < '0' || c > '9' {
				return 0, fmt.Errorf("invalid port %q", addr[i+1:])
			}
			p = p*10 + int(c-'0')
		}
		if p <= 0 || p > 65535 {
			return 0, fmt.Errorf("port out of range: %d", p)
		}
		return p, nil
	}
	return 0, fmt.Errorf("no port in addr %q", addr)
}

// Force-import wf so that future Plan 4 work doesn't bounce on go.mod.
// The package is also referenced by wfp_windows.go and wfp_install_windows.go,
// so this is a no-op at compile time but signals intent.
var _ = wf.ActionPermit
