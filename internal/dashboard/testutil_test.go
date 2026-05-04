package dashboard

import (
	"io"
	"net/http"
	"testing"
	"time"

	"agent-gate/internal/types"
	"github.com/stretchr/testify/require"
)

// seed describes one event to populate before serving.
type seed struct {
	id, url, body string
	startedAt     time.Time // zero → time.Now()
}

// seedEvent builds a seed using time.Now() as StartedAt.
func seedEvent(id, url, body string) seed {
	return seed{id: id, url: url, body: body}
}

// seedEventAt is like seedEvent but lets the caller pin StartedAt.
// Tasks 10+ use this for time-window filter tests.
func seedEventAt(id, url, body string, ts time.Time) seed {
	return seed{id: id, url: url, body: body, startedAt: ts}
}

// testServer wires a fresh dashboard handler with the given seed events
// already appended to its store. Each seed is fed through Store.Append so
// the full PII-indexing pipeline runs (Tasks 1-5 wiring), giving filter
// tests realistic event_pii rows.
func testServer(t *testing.T, seeds ...seed) http.Handler {
	t.Helper()
	opts := freshOpts(t)
	for _, s := range seeds {
		ts := s.startedAt
		if ts.IsZero() {
			ts = time.Now()
		}
		ev := types.StoredEvent{
			ParsedEvent: types.ParsedEvent{
				RawFlow: types.RawFlow{
					ID:         s.id,
					URL:        s.url,
					Method:     "POST",
					ReqHeaders: http.Header{"Content-Type": []string{"application/json"}},
					ReqBody:    []byte(s.body),
					StartedAt:  ts,
				},
				Kind: "generic",
			},
		}
		require.NoError(t, opts.Store.Append(ev))
	}
	return NewServer(opts)
}

// readAll reads the body or fails the test.
func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(b)
}
