package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"agent-gate/internal/allowlist"
	"agent-gate/internal/dismissals"
	"agent-gate/internal/store"
	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// freshOpts wires a Server against a tempdir-backed store + empty allowlist + empty dismissals.
func freshOpts(t *testing.T) Options {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir, time.Now)
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	al, err := allowlist.Load(filepath.Join(dir, "a.txt"))
	require.NoError(t, err)
	di, err := dismissals.Load(filepath.Join(dir, "d.json"))
	require.NoError(t, err)
	return Options{Store: st, Allowlist: al, Dismissals: di}
}

func TestServerServesStaticAssets(t *testing.T) {
	srv := httptest.NewServer(NewServer(freshOpts(t)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/static/htmx.min.js")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "javascript")
	body, _ := io.ReadAll(resp.Body)
	assert.Greater(t, len(body), 1000)
}

func TestServerRendersFullPageOnGetRoot(t *testing.T) {
	srv := httptest.NewServer(NewServer(freshOpts(t)))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "<html")
	assert.Contains(t, bodyStr, "agent-gate")
	assert.Contains(t, bodyStr, `src="/static/htmx.min.js"`)
}

func TestServerRefusesNonLoopbackBind(t *testing.T) {
	opts := freshOpts(t)
	opts.Addr = "0.0.0.0:7878"
	err := Run(opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loopback")
}

func TestSessionsListShowsRowFromStore(t *testing.T) {
	opts := freshOpts(t)
	require.NoError(t, opts.Store.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow:   types.RawFlow{ID: "evt-1", Method: "POST", URL: "https://api.anthropic.com/v1/messages", RespStatus: 200, StartedAt: time.Now()},
		Kind:      "anthropic_messages",
		SessionID: "sess-A",
	}}))

	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "sess-A")
	assert.Contains(t, bodyStr, "api.anthropic.com")
}
