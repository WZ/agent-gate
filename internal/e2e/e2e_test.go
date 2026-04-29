package e2e

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-gate/internal/ca"
	"agent-gate/internal/idgen"
	"agent-gate/internal/parser"
	"agent-gate/internal/proxy"
	"agent-gate/internal/store"
	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndToEndAnthropicLikeRequest(t *testing.T) {
	// Stand up a fake "Anthropic" upstream that returns a Messages-shaped JSON.
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{
			"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-4-7",
			"content":[{"type":"text","text":"hi"}],
			"usage":{"input_tokens":15,"output_tokens":4,"cache_read_input_tokens":1}
		}`))
	}))
	defer upstream.Close()

	// CA + store + proxy.
	caRoot, err := ca.Ensure(t.TempDir())
	require.NoError(t, err)
	dataDir := t.TempDir()
	st, err := store.Open(dataDir, time.Now)
	require.NoError(t, err)
	defer st.Close()

	flowCh := make(chan types.RawFlow, 8)
	go func() {
		for f := range flowCh {
			ev := parser.Parse(f)
			require.NoError(t, st.Append(types.StoredEvent{ParsedEvent: ev}))
		}
	}()

	upstreamPool := x509.NewCertPool()
	upstreamPool.AddCert(upstream.Certificate())

	pAddr := startProxyForE2E(t, proxy.Options{
		Addr:            "127.0.0.1:0",
		CA:              caRoot,
		Out:             flowCh,
		IDGen:           idgen.NewGenerator(),
		CaptureMode:     "permissive",
		UpstreamRootCAs: upstreamPool,
	})

	// Client trusts our root CA only.
	clientPool := x509.NewCertPool()
	clientPool.AddCert(caRoot.Cert)
	pURL, _ := url.Parse("http://" + pAddr)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(pURL),
			TLSClientConfig: &tls.Config{RootCAs: clientPool},
		},
		Timeout: 5 * time.Second,
	}

	// Use the upstream's real URL. The parser's "anthropic_messages" branch only triggers when
	// host == "api.anthropic.com", so this exchange is captured under Kind="generic" — which
	// is what we want to verify here (the wiring works regardless of host).
	uri := upstream.URL + "/v1/messages"

	req, _ := http.NewRequest("POST", uri, strings.NewReader(`{"model":"claude-opus-4-7"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	// Wait briefly for the pipeline goroutine to persist.
	time.Sleep(100 * time.Millisecond)

	rows, err := st.Index().Query(store.QueryFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "POST", rows[0].Method)
	assert.Equal(t, 200, rows[0].Status)
	assert.Equal(t, "permissive", rows[0].CaptureMode)

	// JSONL exists and contains the body.
	files, err := os.ReadDir(dataDir)
	require.NoError(t, err)
	var foundJSONL bool
	for _, f := range files {
		if filepath.Ext(f.Name()) == ".jsonl" {
			foundJSONL = true
			data, err := os.ReadFile(filepath.Join(dataDir, f.Name()))
			require.NoError(t, err)
			var ev types.StoredEvent
			require.NoError(t, json.Unmarshal(data[:len(data)-1], &ev)) // strip trailing newline
			assert.Equal(t, rows[0].ID, ev.ID)
		}
	}
	assert.True(t, foundJSONL)
}

func startProxyForE2E(t *testing.T, opts proxy.Options) string {
	t.Helper()
	ln, err := net.Listen("tcp", opts.Addr)
	require.NoError(t, err)
	opts.Listener = ln
	go func() { _ = proxy.Run(opts) }()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}
