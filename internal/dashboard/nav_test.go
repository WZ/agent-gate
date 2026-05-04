package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBaseTemplateRendersTopNav(t *testing.T) {
	srv := httptest.NewServer(NewServer(freshOpts(t)))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	body := string(raw)

	assert.Contains(t, body, `class="top-nav"`)
	assert.Contains(t, body, `>Operations<`)
	assert.Contains(t, body, `>Explore<`)
	// Sessions list is the active page → the Operations link gets aria-current.
	assert.Contains(t, body,
		`href="/" class="top-nav-link active"`,
		"Operations link should be marked active on the sessions list page")
}
