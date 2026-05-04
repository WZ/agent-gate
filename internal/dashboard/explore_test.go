package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
