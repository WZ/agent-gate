package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
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

func TestSessionDetailListsEvents(t *testing.T) {
	opts := freshOpts(t)
	for i, id := range []string{"e1", "e2", "e3"} {
		require.NoError(t, opts.Store.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{ID: id, Method: "POST", URL: "https://api.anthropic.com/v1/messages",
				RespStatus: 200, StartedAt: time.Date(2026, 4, 29, 0, i, 0, 0, time.UTC)},
			SessionID: "sess-A",
		}}))
	}

	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/sessions/sess-A")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	bodyStr := string(body)
	for _, id := range []string{"e1", "e2", "e3"} {
		assert.Contains(t, bodyStr, id)
	}
}

func TestEventDetailDefaultsToRedacted(t *testing.T) {
	opts := freshOpts(t)
	body := `{"prompt":"my key sk-ant-` + strings.Repeat("a", 60) + `"}`
	require.NoError(t, opts.Store.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow: types.RawFlow{
			ID: "e", Method: "POST", URL: "https://api.anthropic.com/v1/messages",
			RespStatus: 200, ReqBody: []byte(body), StartedAt: time.Now(),
		},
		SessionID: "sess-A",
	}}))

	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events/e")
	require.NoError(t, err)
	bs, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	bodyStr := string(bs)
	assert.NotContains(t, bodyStr, "sk-ant-aaaaaaaa")
	assert.Contains(t, bodyStr, "REDACTED")
}

func TestEventDetailRawShowsBytesAndLogsDismissal(t *testing.T) {
	opts := freshOpts(t)
	body := `{"prompt":"my key sk-ant-` + strings.Repeat("a", 60) + `"}`
	require.NoError(t, opts.Store.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow: types.RawFlow{
			ID: "e", Method: "POST", URL: "https://api.anthropic.com/v1/messages",
			RespStatus: 200, ReqBody: []byte(body), StartedAt: time.Now(),
		},
		SessionID: "sess-A",
	}}))

	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events/e?raw=1")
	require.NoError(t, err)
	bs, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	bodyStr := string(bs)
	assert.Contains(t, bodyStr, "sk-ant-aaaaaaaa", "raw view should expose secrets")
	assert.Contains(t, bodyStr, "raw view")

	entries := opts.Dismissals.Entries()
	require.Len(t, entries, 1)
	assert.Equal(t, "raw_peek", entries[0].Code)
	assert.Equal(t, "e", entries[0].EventID)
}

func TestDismissFlag(t *testing.T) {
	opts := freshOpts(t)
	require.NoError(t, opts.Store.Append(types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{ID: "e", URL: "https://x.com/y", StartedAt: time.Now()},
		},
		Flags: []types.Flag{{Code: "host_not_allowlisted", Severity: "high", Detail: "x.com"}},
	}))

	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	form := url.Values{
		"event_id": {"e"},
		"code":     {"host_not_allowlisted"},
		"host":     {"x.com"},
		"scope":    {"host_code"},
		"reason":   {"private monitoring"},
	}
	resp, err := http.PostForm(srv.URL+"/api/dismiss", form)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)

	assert.True(t, opts.Dismissals.Has("any", "host_not_allowlisted", "x.com"))
}

func TestTrustHostAddsToAllowlist(t *testing.T) {
	opts := freshOpts(t)
	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	resp, err := http.PostForm(srv.URL+"/api/trust", url.Values{"host": {"safe.example.com"}})
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	assert.True(t, opts.Allowlist.Contains("safe.example.com"))
}
