package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
