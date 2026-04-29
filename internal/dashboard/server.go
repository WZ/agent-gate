package dashboard

import (
	"embed"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"

	"agent-gate/internal/allowlist"
	"agent-gate/internal/dismissals"
	"agent-gate/internal/store"
)

//go:embed all:templates all:static
var assets embed.FS

// Options are the dependencies for a dashboard server.
type Options struct {
	Addr       string                 // "127.0.0.1:7878". Required for Run; optional for NewServer.
	Listener   net.Listener           // Optional pre-bound listener; tests use this.
	Store      *store.Store           // Required.
	Allowlist  *allowlist.Allowlist   // Required.
	Dismissals *dismissals.Dismissals // Required.
}

// NewServer returns an http.Handler for the dashboard. Embeds templates + static assets.
func NewServer(opts Options) http.Handler {
	r := newRenderer()
	mux := http.NewServeMux()

	// Static assets.
	staticFS, _ := fs.Sub(assets, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Routes.
	mux.HandleFunc("/", handleSessionsList(opts, r))
	mux.HandleFunc("/sessions/", handleSessionDetail(opts, r))
	mux.HandleFunc("/events/", handleEventDetail(opts, r))

	return mux
}

// Run starts the dashboard listening on opts.Addr. Refuses non-loopback addresses.
// Blocks until the listener closes.
func Run(opts Options) error {
	addr := opts.Addr
	if !isLoopback(addr) {
		return errors.New("dashboard: refusing to bind non-loopback addr " + addr + " (security policy)")
	}
	ln := opts.Listener
	if ln == nil {
		var err error
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			return err
		}
	}
	srv := &http.Server{Handler: NewServer(opts), ReadHeaderTimeout: 30 * time.Second}
	return srv.Serve(ln)
}

func isLoopback(addr string) bool {
	if addr == "" {
		return false
	}
	for _, prefix := range []string{"127.", "::1", "localhost:", "[::1]:"} {
		if strings.HasPrefix(addr, prefix) {
			return true
		}
	}
	return false
}
