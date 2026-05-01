package launcher

import (
	"context"
	"errors"
	"io"
	"net"
)

// Mode is the capture posture: airtight (kernel-enforced) or permissive (env-only).
type Mode string

const (
	Airtight   Mode = "airtight"
	Permissive Mode = "permissive"
)

// ErrUnsupported is returned by Run when airtight is requested on a platform
// without a sandbox implementation.
var ErrUnsupported = errors.New("launcher: airtight unsupported on this platform")

// Options configures one invocation of `agent-gate run`.
type Options struct {
	Mode          Mode
	AirtightFail  bool   // --airtight=fail: abort if airtight unsupported
	ProxyAddr     string // "127.0.0.1:8888"
	DashboardAddr string // "127.0.0.1:7878"
	ConfigPath    string
	Cmd           string
	Args          []string
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	Env           []string

	// Test seams. Production code never sets these.
	proxyHook     func(error) // called if proxy goroutine panics
	dashboardHook func(error) // ditto for dashboard

	// Internal: supervisor → linux spawn channel for the netns listener FD.
	// Exposed unexported only because Go build tags restrict where the field
	// is set. Production callers do not touch this.
	nsListener chan net.Listener
}

// Run blocks until the child exits. Returns the child's exit code.
// On supervisor failure (proxy panic, jail setup error), returns a non-zero
// exit code with the underlying error.
func Run(ctx context.Context, opts Options) (exitCode int, err error) {
	if opts.Mode == "" {
		opts.Mode = Airtight
	}
	if opts.Cmd == "" {
		return 1, errors.New("launcher: Options.Cmd is required")
	}
	return runSupervised(ctx, opts)
}
