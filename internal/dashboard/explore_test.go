package dashboard

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExploreRendersAllEvents(t *testing.T) {
	srv := httptest.NewServer(testServer(t,
		seedEvent("01EXPA", "https://api.anthropic.com/v1/messages",
			`{"email":"alice@example.com"}`),
		seedEvent("01EXPB", "https://api.openai.com/v1/chat",
			`{"phone":"(415) 555-1234"}`),
	))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/explore")
	require.NoError(t, err)
	defer res.Body.Close()
	body := readAll(t, res.Body)
	assert.Contains(t, body, "Explore")
	assert.Contains(t, body, "01EXPA")
	assert.Contains(t, body, "01EXPB")
	assert.Contains(t, body, "api.anthropic.com")
	assert.Contains(t, body, "api.openai.com")
}

func TestExploreWrapsResultsTableForNarrowScreens(t *testing.T) {
	srv := httptest.NewServer(testServer(t,
		seedEvent("01WRAP", "https://api.anthropic.com/v1/messages",
			`{"email":"alice@example.com"}`),
	))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/explore")
	require.NoError(t, err)
	defer res.Body.Close()
	body := readAll(t, res.Body)

	assert.Contains(t, body, `<div class="table-wrap">`)
	assert.Contains(t, body, `<table class="data-table results-table">`)
}

func TestExploreFiltersByKind(t *testing.T) {
	srv := httptest.NewServer(testServer(t,
		seedEvent("01KIND_E", "https://api.anthropic.com/v1/messages",
			`{"email":"alice@example.com"}`),
		seedEvent("01KIND_P", "https://api.openai.com/v1/chat",
			`{"phone":"(415) 555-1234"}`),
		seedEvent("01KIND_PLAIN", "https://example.com/health",
			`{"status":"ok"}`),
	))
	defer srv.Close()

	// Only events with phone PII should appear.
	res, err := http.Get(srv.URL + "/explore?kinds=phone")
	require.NoError(t, err)
	defer res.Body.Close()
	body := readAll(t, res.Body)

	assert.NotContains(t, body, "01KIND_E", "email-only event should be filtered out")
	assert.Contains(t, body, "01KIND_P")
	assert.NotContains(t, body, "01KIND_PLAIN")
}

func TestExploreFiltersByTimeRange(t *testing.T) {
	now := time.Now()
	srv := httptest.NewServer(testServer(t,
		seedEventAt("01OLD", "https://example.com/", `{"x":1}`,
			now.Add(-48*time.Hour)),
		seedEventAt("01RECENT", "https://example.com/", `{"x":1}`,
			now.Add(-30*time.Minute)),
	))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/explore?preset=1h")
	require.NoError(t, err)
	defer res.Body.Close()
	body := readAll(t, res.Body)
	assert.Contains(t, body, "01RECENT")
	assert.NotContains(t, body, "01OLD")
}

func TestExploreFiltersByHost(t *testing.T) {
	srv := httptest.NewServer(testServer(t,
		seedEvent("01HOSTA", "https://api.anthropic.com/v1/messages", `{"x":1}`),
		seedEvent("01HOSTB", "https://api.openai.com/v1/chat", `{"x":1}`),
	))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/explore?host=api.anthropic.com&preset=all")
	require.NoError(t, err)
	defer res.Body.Close()
	body := readAll(t, res.Body)
	assert.Contains(t, body, "01HOSTA")
	assert.NotContains(t, body, "01HOSTB")
}

func TestExploreSearchBodyMatchesAndSnippets(t *testing.T) {
	srv := httptest.NewServer(testServer(t,
		seedEvent("01SRCH", "https://api.example.com/v1/x",
			`{"prompt":"please contact alice@example.com today"}`),
		seedEvent("01SRCH_OTHER", "https://api.example.com/v1/y",
			`{"prompt":"unrelated payload"}`),
	))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/explore?q=alice&preset=all")
	require.NoError(t, err)
	defer res.Body.Close()
	body := readAll(t, res.Body)

	assert.Contains(t, body, "01SRCH")
	assert.NotContains(t, body, "01SRCH_OTHER")
	assert.Contains(t, body, `<mark>alice</mark>`)
	// Surrounding context should be present.
	assert.Contains(t, body, "contact <mark>alice</mark>@example.com")
}

func TestExploreSearchFormPreservesFlagFilter(t *testing.T) {
	opts := freshOpts(t)
	require.NoError(t, opts.Store.Append(types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:        "01FLAGFORM",
				Method:    "POST",
				URL:       "https://api.example.com/v1/x",
				ReqBody:   []byte(`{"prompt":"flagged search target"}`),
				StartedAt: time.Now(),
			},
			Kind: "generic",
		},
		Flags: []types.Flag{{Code: "host_not_allowlisted", Severity: "high"}},
	}))
	srv := httptest.NewServer(NewServer(opts))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/explore?flag=host_not_allowlisted&preset=all")
	require.NoError(t, err)
	defer res.Body.Close()
	body := readAll(t, res.Body)

	assert.Contains(t, body, `name="flag"`)
	assert.Contains(t, body, `value="host_not_allowlisted"`)
}

func TestExploreCombinesAllFilters(t *testing.T) {
	now := time.Now()
	srv := httptest.NewServer(testServer(t,
		seedEventAt("01ALL_HIT", "https://api.anthropic.com/v1/messages",
			`{"email":"alice@example.com","prompt":"hello world"}`,
			now.Add(-30*time.Minute)),
		seedEventAt("01ALL_OLD", "https://api.anthropic.com/v1/messages",
			`{"email":"alice@example.com"}`,
			now.Add(-48*time.Hour)),
		seedEventAt("01ALL_NOPII", "https://api.anthropic.com/v1/messages",
			`{"x":"hello world"}`,
			now.Add(-30*time.Minute)),
	))
	defer srv.Close()

	res, err := http.Get(srv.URL +
		"/explore?preset=1h&kinds=email&host=api.anthropic.com&q=hello")
	require.NoError(t, err)
	defer res.Body.Close()
	body := readAll(t, res.Body)

	assert.Contains(t, body, "01ALL_HIT", "matches all four filters")
	assert.NotContains(t, body, "01ALL_OLD", "outside time window")
	assert.NotContains(t, body, "01ALL_NOPII", "no email PII in body")
	// PII chip should render alongside the row.
	assert.Contains(t, body, `pii-chip-identifying`)
	assert.Contains(t, body, `>1 Email<`)
}

func TestExplorePagination(t *testing.T) {
	// Seed 75 events; default limit is 50. Page 1 shows 50; page 2 shows 25.
	now := time.Now()
	seeds := make([]seed, 0, 75)
	for i := 0; i < 75; i++ {
		seeds = append(seeds, seedEventAt(
			fmt.Sprintf("01PG%02d", i),
			"https://api.example.com/x",
			`{"x":1}`,
			now.Add(-time.Duration(i)*time.Minute),
		))
	}
	srv := httptest.NewServer(testServer(t, seeds...))
	defer srv.Close()

	resPg1, err := http.Get(srv.URL + "/explore?preset=all&page=1")
	require.NoError(t, err)
	defer resPg1.Body.Close()
	pg1 := readAll(t, resPg1.Body)
	assert.Contains(t, pg1, "01PG00")
	assert.Contains(t, pg1, "01PG49")
	assert.NotContains(t, pg1, "01PG50")

	resPg2, err := http.Get(srv.URL + "/explore?preset=all&page=2")
	require.NoError(t, err)
	defer resPg2.Body.Close()
	pg2 := readAll(t, resPg2.Body)
	assert.NotContains(t, pg2, "01PG00")
	assert.Contains(t, pg2, "01PG50")
	assert.Contains(t, pg2, "01PG74")
}

func TestExplorePaginationIncludesEventsPastFirstFiveHundred(t *testing.T) {
	now := time.Now()
	seeds := make([]seed, 0, 525)
	for i := 0; i < 525; i++ {
		seeds = append(seeds, seedEventAt(
			fmt.Sprintf("01BIG%03d", i),
			"https://api.example.com/x",
			`{"x":1}`,
			now.Add(-time.Duration(i)*time.Minute),
		))
	}
	srv := httptest.NewServer(testServer(t, seeds...))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/explore?preset=all&page=11")
	require.NoError(t, err)
	defer res.Body.Close()
	body := readAll(t, res.Body)
	assert.Contains(t, body, "01BIG500")
	assert.Contains(t, body, "01BIG524")
}

func TestExploreSearchIncludesEventsPastFirstFiveHundred(t *testing.T) {
	now := time.Now()
	seeds := make([]seed, 0, 501)
	for i := 0; i < 501; i++ {
		body := `{"x":1}`
		if i == 500 {
			body = `{"prompt":"needle appears only in the oldest event"}`
		}
		seeds = append(seeds, seedEventAt(
			fmt.Sprintf("01SEARCH%03d", i),
			"https://api.example.com/x",
			body,
			now.Add(-time.Duration(i)*time.Minute),
		))
	}
	srv := httptest.NewServer(testServer(t, seeds...))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/explore?preset=all&q=needle")
	require.NoError(t, err)
	defer res.Body.Close()
	body := readAll(t, res.Body)
	assert.Contains(t, body, "01SEARCH500")
	assert.Contains(t, body, "<mark>needle</mark>")
}

func TestExploreFilterLinksRenderUsableQueryStrings(t *testing.T) {
	srv := httptest.NewServer(testServer(t,
		seedEvent("01LINK", "https://api.example.com/x", `{"email":"alice@example.com"}`),
	))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/explore?preset=all")
	require.NoError(t, err)
	defer res.Body.Close()
	body := readAll(t, res.Body)
	assert.Contains(t, body, `href="/explore?preset=1h"`)
	assert.Contains(t, body, `href="/explore?kinds=email&amp;preset=all"`)
}
