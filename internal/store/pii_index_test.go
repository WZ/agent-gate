package store

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"agent-gate/internal/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaIncludesEventPIITable(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	// Open succeeded means the schema executed cleanly. Verify the table
	// is queryable.
	row := s.Index().db.QueryRow(`SELECT count(*) FROM event_pii`)
	var n int
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, 0, n)
}

func TestSchemaIncludesEventPIIIndex(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	row := s.Index().db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_event_pii_code'`)
	var name string
	require.NoError(t, row.Scan(&name))
	assert.Equal(t, "idx_event_pii_code", name)
}

func TestIndexPIIWritesCountsForEvent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	ev := types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:          "01EVT1",
				URL:         "https://api.example.com/v1/contact",
				Method:      "POST",
				ReqHeaders:  http.Header{"Content-Type": []string{"application/json"}},
				ReqBody:     []byte(`{"email":"alice@example.com","note":"call me at (415) 555-1234"}`),
				RespHeaders: http.Header{"Content-Type": []string{"application/json"}},
				RespBody:    []byte(`{"ok":true}`),
			},
			Kind: "generic",
		},
	}

	require.NoError(t, s.indexPII(ev))

	rows, err := s.Index().db.Query(
		`SELECT side, code, count FROM event_pii WHERE event_id = ? ORDER BY side, code`, "01EVT1")
	require.NoError(t, err)
	defer rows.Close()

	type rowT struct {
		side, code string
		count      int
	}
	var got []rowT
	for rows.Next() {
		var r rowT
		require.NoError(t, rows.Scan(&r.side, &r.code, &r.count))
		got = append(got, r)
	}
	require.NoError(t, rows.Err())

	// Request body has 1 email + 1 phone; response has none.
	assert.ElementsMatch(t, []rowT{
		{"req", "email", 1},
		{"req", "phone", 1},
	}, got)
}

func TestIndexPIIIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	ev := types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:         "01IDEMP",
				URL:        "https://api.example.com/x",
				Method:     "POST",
				ReqHeaders: http.Header{"Content-Type": []string{"application/json"}},
				ReqBody:    []byte(`{"email":"a@b.co"}`),
			},
			Kind: "generic",
		},
	}

	require.NoError(t, s.indexPII(ev))
	require.NoError(t, s.indexPII(ev))

	row := s.Index().db.QueryRow(
		`SELECT count FROM event_pii WHERE event_id = ? AND side = 'req' AND code = 'email'`, "01IDEMP")
	var n int
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, 1, n, "second indexPII should REPLACE, not duplicate")
}

func TestReindexPIIRebuildsFromJSONL(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	for _, body := range []string{
		`{"email":"a@b.co"}`,
		`{"phone":"(415) 555-1234"}`,
		`{"plain":"text"}`,
	} {
		ev := types.StoredEvent{
			ParsedEvent: types.ParsedEvent{
				RawFlow: types.RawFlow{
					ID:         body, // unique per test row
					URL:        "https://api.example.com/x",
					Method:     "POST",
					ReqHeaders: http.Header{"Content-Type": []string{"application/json"}},
					ReqBody:    []byte(body),
				},
				Kind: "generic",
			},
		}
		require.NoError(t, s.Append(ev))
	}

	// Wipe the table to simulate a corrupted index.
	_, err = s.Index().db.Exec(`DELETE FROM event_pii`)
	require.NoError(t, err)

	// Sanity check: the table is empty.
	row := s.Index().db.QueryRow(`SELECT count(*) FROM event_pii`)
	var before int
	require.NoError(t, row.Scan(&before))
	require.Equal(t, 0, before, "precondition: event_pii is empty")

	// Reindex.
	require.NoError(t, s.ReindexPII(context.Background()))

	// We should now see exactly the same counts indexPII would have written.
	row = s.Index().db.QueryRow(`SELECT count(*) FROM event_pii`)
	var after int
	require.NoError(t, row.Scan(&after))
	assert.Equal(t, 2, after, "two events have PII (email + phone); third has none")
}

// jsonlLineForEvent is a tiny helper to round-trip a StoredEvent through JSON
// in case future tests need to assemble JSONL state by hand.
func jsonlLineForEvent(t *testing.T, ev types.StoredEvent) []byte {
	t.Helper()
	b, err := json.Marshal(ev)
	require.NoError(t, err)
	return append(b, '\n')
}
