package store

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreAppendIndexesAndPersists(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	ev := types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow:   types.RawFlow{ID: "01HX", Method: "POST", URL: "https://api.anthropic.com/v1/messages"},
		Kind:      "anthropic_messages",
		SessionID: "sess-1",
	}}
	require.NoError(t, s.Append(ev))

	// Index should have it.
	rows, err := s.Index().Query(QueryFilter{SessionID: "sess-1"})
	require.NoError(t, err)
	require.Len(t, rows, 1)

	// Body retrieval by ID should return the JSON line.
	r, err := s.Body("01HX")
	require.NoError(t, err)
	defer r.Close()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"id":"01HX"`)
	assert.True(t, strings.HasSuffix(string(data), "\n"))
}

func TestStoreClearWipesEventsAndJSONL(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 4, 29, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	// Append a few events.
	for _, id := range []string{"e1", "e2", "e3"} {
		require.NoError(t, s.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{ID: id, URL: "https://example.com/", Method: "GET"},
			Kind:    "generic",
		}}))
	}

	rows, err := s.Index().Query(QueryFilter{Limit: 10})
	require.NoError(t, err)
	require.Len(t, rows, 3, "precondition: events appended")

	// Files should exist.
	entries, _ := os.ReadDir(dir)
	var jsonlBefore int
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			jsonlBefore++
		}
	}
	require.Equal(t, 1, jsonlBefore, "precondition: one jsonl file")

	// Clear.
	require.NoError(t, s.Clear())

	// Index empty.
	rows, err = s.Index().Query(QueryFilter{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, rows, "index should be empty after Clear")

	// No JSONL files.
	entries, _ = os.ReadDir(dir)
	for _, e := range entries {
		assert.NotEqual(t, ".jsonl", filepath.Ext(e.Name()),
			"no jsonl files should remain after Clear")
	}

	// Subsequent Append should work (writer state reset cleanly).
	require.NoError(t, s.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow: types.RawFlow{ID: "post-clear", URL: "https://example.com/x", Method: "GET"},
		Kind:    "generic",
	}}))
	rows, err = s.Index().Query(QueryFilter{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, rows, 1, "Append after Clear should work")
}

func TestStoreOpenInitializesPaths(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, time.Now)
	require.NoError(t, err)
	defer s.Close()
	// Both events.db and a JSONL dir should be writable.
	_, err = s.JSONLWriter().Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{RawFlow: types.RawFlow{ID: "x"}}})
	require.NoError(t, err)
	_, err = filepath.Rel(dir, s.IndexPath())
	require.NoError(t, err)
}

func TestStoreAppendPopulatesEventPII(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	ev := types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:         "01APPND",
				URL:        "https://api.example.com/v1/x",
				Method:     "POST",
				ReqHeaders: http.Header{"Content-Type": []string{"application/json"}},
				ReqBody:    []byte(`{"email":"alice@example.com"}`),
			},
			Kind: "generic",
		},
	}
	require.NoError(t, s.Append(ev))

	row := s.Index().db.QueryRow(
		`SELECT count FROM event_pii WHERE event_id = ? AND side = 'req' AND code = 'email'`, "01APPND")
	var n int
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, 1, n)
}

func TestStoreAppendBestEffortOnPIIError(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	// Drop the event_pii table mid-flight to force an error on the next indexPII.
	_, err = s.Index().db.Exec(`DROP TABLE event_pii`)
	require.NoError(t, err)

	ev := types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:         "01ERR",
				URL:        "https://api.example.com/v1/x",
				Method:     "POST",
				ReqHeaders: http.Header{"Content-Type": []string{"application/json"}},
				ReqBody:    []byte(`{"email":"alice@example.com"}`),
			},
			Kind: "generic",
		},
	}
	// Append must STILL succeed — audit-log completeness wins over PII metadata.
	require.NoError(t, s.Append(ev))

	// And the event row in the events table must exist.
	rows, err := s.Index().Query(QueryFilter{Limit: 100})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "01ERR", rows[0].ID)
}
