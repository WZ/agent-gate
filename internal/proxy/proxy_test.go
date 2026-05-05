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

func TestProxyWithUpstreamInsecureSkipVerify(t *testing.T) {
	upstream := startUpstream(t)

	root, err := ca.Ensure(t.TempDir())
	require.NoError(t, err)

	out := make(chan types.RawFlow, 8)
	pAddr := startProxy(t, Options{
		Addr:                       "127.0.0.1:0",
		CA:                         root,
		Out:                        out,
		IDGen:                      idgen.NewGenerator(),
		CaptureMode:                "permissive",
		UpstreamInsecureSkipVerify: true,
		// NOTE: NOT setting UpstreamRootCAs, so the proxy can't verify the upstream cert normally.
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

// TestProxyHostGuardBlocksAndRecords verifies that when HostGuard returns
// true for a request's host, the proxy synthesizes a 403 Forbidden response
// (without contacting upstream) AND still emits the flow on the Out channel.
func TestProxyHostGuardBlocksAndRecords(t *testing.T) {
	upstream := startUpstream(t)

	root, err := ca.Ensure(t.TempDir())
	require.NoError(t, err)

	// Hit-counter so we can assert the upstream is NOT contacted on a block.
	upstreamHits := 0
	upstreamGuard := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamHits++
		w.WriteHeader(200)
	}))
	t.Cleanup(upstreamGuard.Close)

	out := make(chan types.RawFlow, 8)
	pAddr := startProxy(t, Options{
		Addr:            "127.0.0.1:0",
		CA:              root,
		Out:             out,
		IDGen:           idgen.NewGenerator(),
		CaptureMode:     "airtight",
		UpstreamRootCAs: upstreamRoots(upstream),
		HostGuard: func(host string) bool {
			// Block everything except the allowed test host.
			return host != "ok.example"
		},
	})

	pool := x509.NewCertPool()
	pool.AddCert(root.Cert)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(mustURL(t, "http://"+pAddr)),
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
		Timeout: 5 * time.Second,
	}

	// Request to upstreamGuard — its hostname is "127.0.0.1", not "ok.example",
	// so HostGuard returns true. Proxy must respond 403 without forwarding.
	resp, err := client.Get(upstreamGuard.URL + "/v1/messages")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	assert.Equal(t, 403, resp.StatusCode)
	assert.Contains(t, string(body), "host not in allowlist")
	assert.Contains(t, string(body), "127.0.0.1")
	assert.Equal(t, "host_not_allowlisted", resp.Header.Get("X-Agent-Gate-Block"))
	assert.Equal(t, 0, upstreamHits, "upstream should NOT be contacted on block")

	// Flow must still land on Out.
	select {
	case f := <-out:
		assert.NotEmpty(t, f.ID)
		assert.Equal(t, 403, f.RespStatus)
		assert.Contains(t, string(f.RespBody), "host not in allowlist")
		assert.Contains(t, string(f.RespBody), "127.0.0.1")
		assert.Equal(t, "airtight", f.CaptureMode)
	case <-time.After(2 * time.Second):
		t.Fatal("expected blocked RawFlow on channel")
	}
}

// TestProxyPassthroughTunnelsRaw verifies that when PassthroughHost returns
// true for a hostname, the proxy returns goproxy.OkConnect (raw TCP tunnel)
// instead of attempting TLS MITM. This is the path used for cert-pinned
// upstreams (mcp-proxy.anthropic.com etc.) where MITM would fail.
//
// We assert this indirectly: with passthrough enabled, our proxy does NOT
// present its CA-signed leaf during TLS — so a client that ONLY trusts our
// CA fails the handshake (because the upstream's real cert isn't CA-signed
// by us). With passthrough disabled, the same client succeeds. The handshake
// outcome is the canary for which path mitmConnect took.
func TestProxyPassthroughTunnelsRaw(t *testing.T) {
	upstream := startUpstream(t)

	root, err := ca.Ensure(t.TempDir())
	require.NoError(t, err)

	// Client trusts ONLY our CA. Upstream uses a self-signed cert that is
	// NOT signed by our CA — so when MITM is on, the proxy presents a
	// leaf signed by our CA and the client succeeds. When passthrough is
	// on, the proxy passes the upstream's real cert through and the client
	// rejects it.
	pool := x509.NewCertPool()
	pool.AddCert(root.Cert)

	// Case 1: passthrough OFF → MITM happens → client succeeds.
	out := make(chan types.RawFlow, 8)
	pAddr := startProxy(t, Options{
		Addr:            "127.0.0.1:0",
		CA:              root,
		Out:             out,
		IDGen:           idgen.NewGenerator(),
		CaptureMode:     "permissive",
		UpstreamRootCAs: upstreamRoots(upstream),
	})
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(mustURL(t, "http://"+pAddr)),
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get(upstream.URL + "/x")
	require.NoError(t, err, "MITM path should succeed when client trusts proxy CA")
	resp.Body.Close()

	// Case 2: passthrough ON → raw tunnel → client sees upstream's REAL
	// cert (self-signed by httptest), which is NOT in our pool → handshake
	// failure. Different error class than the MITM path.
	out2 := make(chan types.RawFlow, 8)
	pAddr2 := startProxy(t, Options{
		Addr:            "127.0.0.1:0",
		CA:              root,
		Out:             out2,
		IDGen:           idgen.NewGenerator(),
		CaptureMode:     "permissive",
		UpstreamRootCAs: upstreamRoots(upstream),
		PassthroughHost: func(host string) bool { return true },
	})
	client2 := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(mustURL(t, "http://"+pAddr2)),
			TLSClientConfig: &tls.Config{RootCAs: pool},
		},
		Timeout: 5 * time.Second,
	}
	_, err = client2.Get(upstream.URL + "/x")
	require.Error(t, err, "passthrough should let upstream's real cert through; client must reject it")
	assert.Contains(t, err.Error(), "x509", "expected x509 verification error, got: %v", err)
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
