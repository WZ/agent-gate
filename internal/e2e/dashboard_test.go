package e2e

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"agent-gate/internal/allowlist"
	"agent-gate/internal/ca"
	"agent-gate/internal/dashboard"
	"agent-gate/internal/dismissals"
	"agent-gate/internal/idgen"
	"agent-gate/internal/parser"
	"agent-gate/internal/policy"
	"agent-gate/internal/proxy"
	"agent-gate/internal/store"
	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startDashboard binds a listener and runs the dashboard, returning its addr.
func startDashboard(t *testing.T, opts dashboard.Options) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	opts.Listener = ln
	opts.Addr = ln.Addr().String()
	go func() { _ = dashboard.Run(opts) }()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func TestE2EDashboardShowsCapturedEventWithFlag(t *testing.T) {
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"msg_1"}`))
	}))
	defer upstream.Close()

	caRoot, err := ca.Ensure(t.TempDir())
	require.NoError(t, err)
	dataDir := t.TempDir()
	st, err := store.Open(dataDir, time.Now)
	require.NoError(t, err)
	defer st.Close()
	configDir := t.TempDir()
	al, _ := allowlist.Load(filepath.Join(configDir, "a.txt"))
	di, _ := dismissals.Load(filepath.Join(configDir, "d.json"))

	// Engine without the upstream host in allowlist → host_not_allowlisted will fire.
	engine := policy.NewEngine(al, di,
		policy.NewHostNotAllowlistedRule(al),
		policy.PermissiveCaptureRule{},
	)

	flowCh := make(chan types.RawFlow, 8)
	parsedDone := make(chan struct{})
	go func() {
		defer close(parsedDone)
		for f := range flowCh {
			ev := parser.Parse(f)
			flags := engine.Evaluate(&ev)
			require.NoError(t, st.Append(types.StoredEvent{ParsedEvent: ev, Flags: flags}))
		}
	}()

	upstreamPool := x509.NewCertPool()
	upstreamPool.AddCert(upstream.Certificate())
	pAddr := startProxyForE2E(t, proxy.Options{
		Addr: "127.0.0.1:0", CA: caRoot, Out: flowCh, IDGen: idgen.NewGenerator(),
		CaptureMode: "permissive", UpstreamRootCAs: upstreamPool,
	})

	clientPool := x509.NewCertPool()
	clientPool.AddCert(caRoot.Cert)
	pURL, _ := url.Parse("http://" + pAddr)
	httpClient := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(pURL),
		TLSClientConfig: &tls.Config{RootCAs: clientPool},
	}, Timeout: 5 * time.Second}

	resp, err := httpClient.Get(upstream.URL + "/v1/messages")
	require.NoError(t, err)
	resp.Body.Close()

	require.Eventually(t, func() bool {
		rows, _ := st.Index().Query(store.QueryFilter{Limit: 5})
		return len(rows) == 1
	}, 5*time.Second, 10*time.Millisecond)

	dAddr := startDashboard(t, dashboard.Options{
		Store: st, Allowlist: al, Dismissals: di,
	})
	dResp, err := http.Get("http://" + dAddr + "/")
	require.NoError(t, err)
	dBody, _ := io.ReadAll(dResp.Body)
	dResp.Body.Close()
	dStr := string(dBody)
	assert.Contains(t, dStr, "127.0.0.1", "should show the upstream host")

	// Trust the host via the API and verify allowlist persistence.
	form := url.Values{"host": {"safe.example.com"}}
	tResp, err := http.PostForm("http://"+dAddr+"/api/trust", form)
	require.NoError(t, err)
	tResp.Body.Close()
	assert.True(t, al.Contains("safe.example.com"))

	close(flowCh)
	<-parsedDone
}
