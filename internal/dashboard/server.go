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
	"agent-gate/internal/denylist"
	"agent-gate/internal/dismissals"
	"agent-gate/internal/passthrough"
	"agent-gate/internal/store"
)

//go:embed all:templates all:static
var assets embed.FS

// Options are the dependencies for a dashboard server.
type Options struct {
	Addr             string                 // "127.0.0.1:7878". Required for Run; optional for NewServer.
	Listener         net.Listener           // Optional pre-bound listener; tests use this.
	Store            *store.Store           // Required.
	Allowlist        *allowlist.Allowlist   // Required.
	Denylist         *denylist.Denylist     // Optional; if nil, "Block this host" returns 503.
	Passthrough      *passthrough.List      // Optional; if nil, "Passthrough" returns 503.
	Dismissals       *dismissals.Dismissals // Required.
	LivePollInterval time.Duration          // optional; defaults to 500ms
}

// SignatureHeader is set on every dashboard response so other agent-gate
// processes can recognize a peer dashboard via a quick HEAD/GET probe
// without scraping HTML. Used by the supervisor when it wants to reuse
// an existing dashboard instead of failing the run on a port collision.
const SignatureHeader = "X-Agent-Gate"

// NewServer returns an http.Handler for the dashboard. Embeds templates + static assets.
func NewServer(opts Options) http.Handler {
	r := newRenderer()
	mux := http.NewServeMux()

	// Static assets.
	staticFS, _ := fs.Sub(assets, "static")
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Some browsers still request /favicon.ico even with <link rel="icon">.
	// Send them to the SVG instead of letting it 404.
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/static/favicon.svg", http.StatusFound)
	})

	// Routes.
	mux.HandleFunc("/", handleSessionsList(opts, r))
	mux.HandleFunc("/sessions/", handleSessionDetail(opts, r))
	mux.HandleFunc("/events/", handleEventDetail(opts, r))
	mux.HandleFunc("/explore", handleExplore(opts, r))
	mux.HandleFunc("/api/dismiss", handleDismiss(opts))
	mux.HandleFunc("/api/trust", handleTrust(opts))
	mux.HandleFunc("/api/untrust", handleUntrust(opts))
	mux.HandleFunc("/api/block", handleBlock(opts))
	mux.HandleFunc("/api/passthrough", handlePassthrough(opts))
	mux.HandleFunc("/api/live", handleLive(opts))
	mux.HandleFunc("/api/clear", handleClear(opts))

	return signatureMiddleware(mux)
}

// signatureMiddleware stamps SignatureHeader on every response so peer
// agent-gate processes can identify us. Set in headers (not body) so
// HEAD probes and any future content-type changes still match.
func signatureMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(SignatureHeader, "1")
		next.ServeHTTP(w, r)
	})
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
