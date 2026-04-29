package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"agent-gate/internal/ca"
	"agent-gate/internal/idgen"
	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startUpstream launches an httptest TLS server that echoes a fixed body.
func startUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"msg_1"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestProxyCapturesRoundTrip(t *testing.T) {
	upstream := startUpstream(t)

	root, err := ca.Ensure(t.TempDir())
	require.NoError(t, err)

	out := make(chan types.RawFlow, 8)
	pAddr := startProxy(t, Options{
		Addr:        "127.0.0.1:0",
		CA:          root,
		Out:         out,
		IDGen:       idgen.NewGenerator(),
		CaptureMode: "permissive",
		// Trust upstream's self-signed cert so the proxy can connect to it.
		UpstreamRootCAs: upstreamRoots(upstream),
	})

	// Build a client that uses our proxy and trusts our root CA.
	pool := x509.NewCertPool()
	pool.AddCert(root.Cert)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(mustURL(t, "http://"+pAddr)),
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(upstream.URL + "/v1/messages")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, string(body), "msg_1")

	// One flow should be on the channel.
	select {
	case f := <-out:
		assert.NotEmpty(t, f.ID)
		assert.Equal(t, "GET", f.Method)
		assert.Equal(t, 200, f.RespStatus)
		assert.Contains(t, string(f.RespBody), "msg_1")
		assert.Equal(t, "permissive", f.CaptureMode)
	case <-time.After(2 * time.Second):
		t.Fatal("expected RawFlow on channel")
	}
}

// helpers
func mustURL(t *testing.T, s string) *url.URL {
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}

func upstreamRoots(srv *httptest.Server) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return pool
}

func startProxy(t *testing.T, opts Options) string {
	t.Helper()
	ln, err := net.Listen("tcp", opts.Addr)
	require.NoError(t, err)
	opts.Listener = ln
	go func() { _ = Run(opts) }()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}
