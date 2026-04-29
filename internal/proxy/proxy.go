package proxy

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"agent-gate/internal/ca"
	"agent-gate/internal/idgen"
	"agent-gate/internal/types"
	"github.com/elazarl/goproxy"
)

// Options configure the proxy.
type Options struct {
	Addr                       string                        // "127.0.0.1:8888". Required if Listener is nil.
	Listener                   net.Listener                  // Optional pre-bound listener (used by tests).
	CA                         *ca.CA                        // Required.
	Out                        chan<- types.RawFlow          // Required: parser inbox.
	IDGen                      *idgen.Generator              // Required.
	CaptureMode                string                        // "airtight" | "permissive". Required.
	UpstreamRootCAs            *x509.CertPool                // Optional: trust roots for upstream TLS (tests use this).
	UpstreamInsecureSkipVerify bool                          // Optional: if true, skip TLS verification on upstream connection. Testing only.
	BodyLimit                  int64                         // Optional: max body size to keep in memory. Default 8 MiB.
	Logger                     func(format string, a ...any) // Optional.
}

const defaultBodyLimit = 8 << 20

// readLimited reads up to limit bytes from r. Returns (buf, truncated, err).
// truncated is true if r had more bytes than limit.
func readLimited(r io.Reader, limit int64) ([]byte, bool, error) {
	if r == nil {
		return nil, false, nil
	}
	buf, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return buf, false, err
	}
	if int64(len(buf)) > limit {
		return buf[:limit], true, nil
	}
	return buf, false, nil
}

// Run starts the proxy and blocks until the listener is closed.
func Run(opts Options) error {
	if opts.CA == nil || opts.Out == nil || opts.IDGen == nil {
		return errors.New("proxy: CA, Out, and IDGen are required")
	}
	if opts.CaptureMode == "" {
		return errors.New("proxy: CaptureMode is required")
	}
	if opts.BodyLimit == 0 {
		opts.BodyLimit = defaultBodyLimit
	}
	if opts.Logger == nil {
		opts.Logger = func(string, ...any) {}
	}

	gp := buildGoproxy(opts)

	if opts.Listener == nil {
		ln, err := net.Listen("tcp", opts.Addr)
		if err != nil {
			return err
		}
		opts.Listener = ln
	}
	srv := &http.Server{Handler: gp, ReadHeaderTimeout: 30 * time.Second}
	return srv.Serve(opts.Listener)
}

func buildGoproxy(opts Options) *goproxy.ProxyHttpServer {
	gp := goproxy.NewProxyHttpServer()

	// Configure goproxy to MITM all CONNECT and use our root CA for leaf signing.
	gp.OnRequest().HandleConnectFunc(mitmConnect(opts))

	// Configure upstream Transport with our optional root pool (for tests; nil in prod).
	gp.Tr = &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:            opts.UpstreamRootCAs,
			InsecureSkipVerify: opts.UpstreamInsecureSkipVerify,
		},
	}

	// Inflight tracker so that the request and response phases can find each other.
	tracker := newTracker()

	gp.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		id := opts.IDGen.New()
		ctx.UserData = id

		reqBytes, reqTrunc, _ := readLimited(req.Body, opts.BodyLimit)
		if req.Body != nil {
			req.Body.Close()
		}
		if reqBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(reqBytes))
		}

		tracker.set(id, &inflight{
			id:           id,
			startedAt:    time.Now(),
			req:          cloneRequestMeta(req),
			reqBody:      reqBytes,
			reqTruncated: reqTrunc,
		})
		return req, nil
	})

	gp.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		id, _ := ctx.UserData.(string)
		inf := tracker.take(id)
		if inf == nil {
			return resp
		}
		var respBytes []byte
		var respTrunc bool
		if resp != nil {
			respBytes, respTrunc, _ = readLimited(resp.Body, opts.BodyLimit)
			if resp.Body != nil {
				resp.Body.Close()
			}
			if respBytes != nil {
				resp.Body = io.NopCloser(bytes.NewReader(respBytes))
			}
		}

		respHeaders := resp.Header.Clone()
		flow := types.RawFlow{
			ID:            inf.id,
			StartedAt:     inf.startedAt,
			EndedAt:       time.Now(),
			Method:        inf.req.Method,
			URL:           inf.req.URL.String(),
			ReqHeaders:    inf.req.Header,
			ReqBody:       inf.reqBody,
			RespStatus:    resp.StatusCode,
			RespHeaders:   respHeaders,
			RespBody:      respBytes,
			IsStreamed:    isSSE(respHeaders),
			BodyTruncated: inf.reqTruncated || respTrunc,
			CaptureMode:   opts.CaptureMode,
		}

		// Audit posture: block rather than drop. The Out channel is buffered by the
		// caller; if the parser/store falls behind, the proxy slows down (and
		// upstream requests may eventually time out), but we never silently lose
		// flows. Drop-on-full would be a correctness bug for an audit tool.
		opts.Out <- flow
		return resp
	})
	return gp
}

func mitmConnect(opts Options) func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
	return func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
		serverName, _, _ := net.SplitHostPort(host)
		if serverName == "" {
			serverName = host
		}
		// Sign a leaf for this hostname using our local CA.
		leaf, err := opts.CA.SignLeaf(serverName)
		if err != nil {
			opts.Logger("proxy: failed to sign leaf for %s: %v", serverName, err)
			return goproxy.RejectConnect, host
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{{
				Certificate: [][]byte{leaf.Cert.Raw, opts.CA.Cert.Raw},
				PrivateKey:  leaf.Key,
			}},
		}
		return &goproxy.ConnectAction{
			Action:    goproxy.ConnectMitm,
			TLSConfig: func(host string, ctx *goproxy.ProxyCtx) (*tls.Config, error) { return tlsCfg, nil },
		}, host
	}
}

// inflight is one in-progress request awaiting its response.
type inflight struct {
	id           string
	startedAt    time.Time
	req          requestMeta
	reqBody      []byte
	reqTruncated bool
}

type requestMeta struct {
	Method string
	URL    *urlPair
	Header http.Header
}

type urlPair struct{ Original string }

func (u *urlPair) String() string { return u.Original }

func cloneRequestMeta(r *http.Request) requestMeta {
	return requestMeta{
		Method: r.Method,
		URL:    &urlPair{Original: r.URL.String()},
		Header: r.Header.Clone(),
	}
}

type tracker struct {
	mu sync.Mutex
	m  map[string]*inflight
}

func newTracker() *tracker { return &tracker{m: map[string]*inflight{}} }

func (t *tracker) set(id string, inf *inflight) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.m[id] = inf
}
func (t *tracker) take(id string) *inflight {
	t.mu.Lock()
	defer t.mu.Unlock()
	inf := t.m[id]
	delete(t.m, id)
	return inf
}

func isSSE(h http.Header) bool {
	for _, v := range h.Values("Content-Type") {
		if strings.Contains(strings.ToLower(v), "text/event-stream") {
			return true
		}
	}
	return false
}
