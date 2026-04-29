package store

import (
	"io"
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
