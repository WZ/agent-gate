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

func TestSessionsListRendersSOCSummary(t *testing.T) {
	opts := freshOpts(t)
	base := time.Date(2026, 5, 1, 15, 30, 0, 0, time.UTC)
	require.NoError(t, opts.Store.Append(types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:          "evt-high",
				Method:      "POST",
				URL:         "https://api.anthropic.com/v1/messages",
				RespStatus:  200,
				StartedAt:   base,
				CaptureMode: "permissive",
			},
			SessionID: "sess-A",
		},
		Flags: []types.Flag{{Code: "secret_in_request", Severity: "high", Detail: "secret"}},
	}))
	require.NoError(t, opts.Store.Append(types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:          "evt-medium",
				Method:      "GET",
				URL:         "https://api.github.com/repos",
				RespStatus:  200,
				StartedAt:   base.Add(-time.Minute),
				CaptureMode: "permissive",
			},
			SessionID: "sess-B",
		},
		Flags: []types.Flag{{Code: "unknown_mcp_endpoint", Severity: "medium", Detail: "stream"}},
	}))
	require.NoError(t, opts.Store.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow: types.RawFlow{
			ID:          "evt-clear",
			Method:      "GET",
			URL:         "https://safe.example.com/ok",
			RespStatus:  200,
			StartedAt:   base.Add(-2 * time.Minute),
			CaptureMode: "permissive",
		},
		SessionID: "sess-C",
	}}))

	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	bodyStr := string(body)

	assert.Contains(t, bodyStr, "SOC Console")
	assert.Contains(t, bodyStr, "Captured events")
	assert.Contains(t, bodyStr, ">3<")
	assert.Contains(t, bodyStr, "Session groups")
	assert.Contains(t, bodyStr, "Flagged groups")
	assert.Contains(t, bodyStr, "High severity")
	assert.Contains(t, bodyStr, "Medium severity")
	assert.Contains(t, bodyStr, "secret_in_request")
	assert.Contains(t, bodyStr, "unknown_mcp_endpoint")
	assert.Contains(t, bodyStr, "2026-05-01 15:30:00")
}

func TestSessionDetailListsEvents(t *testing.T) {
	opts := freshOpts(t)
	for i, id := range []string{"e1", "e2", "e3"} {
		stored := types.StoredEvent{ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{ID: id, Method: "POST", URL: "https://api.anthropic.com/v1/messages",
				RespStatus: 200, StartedAt: time.Date(2026, 4, 29, 0, i, 0, 0, time.UTC)},
			SessionID: "sess-A",
		}}
		if id == "e2" {
			stored.Flags = []types.Flag{{Code: "host_not_allowlisted", Severity: "high", Detail: "api.anthropic.com"}}
		}
		require.NoError(t, opts.Store.Append(stored))
	}

	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/sessions/sess-A")
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	bodyStr := string(body)
	assert.Contains(t, bodyStr, "Investigation timeline")
	assert.Contains(t, bodyStr, "Total events")
	assert.Contains(t, bodyStr, ">3<")
	assert.Contains(t, bodyStr, "Flag hits")
	assert.Contains(t, bodyStr, ">1<")
	assert.Contains(t, bodyStr, "latest event 2026-04-29 00:02:00")
	assert.Contains(t, bodyStr, "host_not_allowlisted")
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

// TestSessionsListGroupsEmptySessionByHost verifies that events with empty SessionID are
// grouped per-host (with port stripped) and appear as separate rows labelled "(host) <h>".
// The sentinel string "(no session)" must NOT appear.
func TestSessionsListGroupsEmptySessionByHost(t *testing.T) {
	opts := freshOpts(t)
	base := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)

	for i, u := range []string{
		"https://api.github.com:443/repos",
		"https://api.github.com:443/issues",
		"https://api.anthropic.com:443/v1/messages",
		"https://api.anthropic.com:443/v1/complete",
	} {
		require.NoError(t, opts.Store.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:         strings.Repeat("g", i+1),
				Method:     "POST",
				URL:        u,
				RespStatus: 200,
				StartedAt:  base.Add(time.Duration(i) * time.Minute),
			},
			// SessionID intentionally left empty.
		}}))
	}

	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	bs, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(bs)

	// Two separate host rows, port stripped.
	assert.Contains(t, body, "(host) api.github.com")
	assert.Contains(t, body, "(host) api.anthropic.com")

	// The old sentinel must not appear.
	assert.NotContains(t, body, "(no session)")

	// Each host should show EventCount=2; the template renders the number directly.
	// We verify by checking the links use the host: prefix key.
	assert.Contains(t, body, `href="/sessions/host:api.github.com"`)
	assert.Contains(t, body, `href="/sessions/host:api.anthropic.com"`)
}

// TestSessionDetailServesHostBucket verifies that GET /sessions/host:<h> returns only
// the events with empty SessionID for that host, not events with a real SessionID.
func TestSessionDetailServesHostBucket(t *testing.T) {
	opts := freshOpts(t)
	base := time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)

	// 3 empty-session events on api.github.com.
	for i := 0; i < 3; i++ {
		require.NoError(t, opts.Store.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:         "empty-" + strings.Repeat("x", i+1),
				Method:     "GET",
				URL:        "https://api.github.com/repos",
				RespStatus: 200,
				StartedAt:  base.Add(time.Duration(i) * time.Minute),
			},
		}}))
	}
	// 1 event on api.github.com with a real SessionID — must NOT appear.
	require.NoError(t, opts.Store.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow: types.RawFlow{
			ID:         "real-event",
			Method:     "POST",
			URL:        "https://api.github.com/gql",
			RespStatus: 200,
			StartedAt:  base.Add(10 * time.Minute),
		},
		SessionID: "real",
	}}))

	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/sessions/host:api.github.com")
	require.NoError(t, err)
	bs, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(bs)

	// All 3 empty-session events should be listed.
	assert.Contains(t, body, "empty-x")
	assert.Contains(t, body, "empty-xx")
	assert.Contains(t, body, "empty-xxx")

	// The real-session event must NOT appear.
	assert.NotContains(t, body, "real-event")
}

func TestClearWipesEvents(t *testing.T) {
	opts := freshOpts(t)
	require.NoError(t, opts.Store.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow: types.RawFlow{ID: "e", URL: "https://x.com/y", StartedAt: time.Now()},
	}}))

	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	// Use a client that doesn't follow redirects so we can see the 303.
	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Post(srv.URL+"/api/clear", "application/x-www-form-urlencoded", nil)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusSeeOther, resp.StatusCode, "should redirect after clear")

	rows, err := opts.Store.Index().Query(store.QueryFilter{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, rows, "events should be empty after clear")
}

func TestClearRequiresPOST(t *testing.T) {
	opts := freshOpts(t)
	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/clear")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 405, resp.StatusCode)
}

func TestSessionsListFilteredByHost(t *testing.T) {
	opts := freshOpts(t)
	require.NoError(t, opts.Store.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow:   types.RawFlow{ID: "a", URL: "https://api.anthropic.com/v1", StartedAt: time.Now()},
		SessionID: "S1",
	}}))
	require.NoError(t, opts.Store.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow:   types.RawFlow{ID: "b", URL: "https://github.com/x", StartedAt: time.Now()},
		SessionID: "S2",
	}}))

	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/?host=github.com")
	require.NoError(t, err)
	bs, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	body := string(bs)
	assert.Contains(t, body, "S2")
	assert.NotContains(t, body, "S1")
}
