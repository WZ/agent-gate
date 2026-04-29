package store

import (
	"path/filepath"
	"testing"

	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLiteOpenCreatesSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	idx, err := OpenIndex(dbPath)
	require.NoError(t, err)
	defer idx.Close()

	// Querying an empty DB should return no rows and no error.
	rows, err := idx.Query(QueryFilter{})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestSQLiteInsertAndQuery(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	idx, err := OpenIndex(dbPath)
	require.NoError(t, err)
	defer idx.Close()

	ev := types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow:   types.RawFlow{ID: "01HX", Method: "POST", URL: "https://api.anthropic.com/v1/messages", RespStatus: 200},
			Kind:      "anthropic_messages",
			SessionID: "sess-1",
			Model:     "claude-opus-4-7",
			Usage:     types.Usage{InputTokens: 100, OutputTokens: 50},
		},
		Flags: []types.Flag{{Code: "permissive_capture", Severity: "info"}},
	}
	loc := Location{Path: "/tmp/2026-04-28.jsonl", Offset: 0, Length: 200}
	require.NoError(t, idx.Insert(ev, loc))

	rows, err := idx.Query(QueryFilter{SessionID: "sess-1"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "01HX", rows[0].ID)
	assert.Equal(t, "claude-opus-4-7", rows[0].Model)
	assert.Equal(t, int64(0), rows[0].JSONLOffset)
	assert.Equal(t, int64(200), rows[0].JSONLLength)
	assert.Equal(t, "permissive_capture", rows[0].FlagCodes)
}

func TestSQLiteFilterByHostAndTime(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	idx, err := OpenIndex(dbPath)
	require.NoError(t, err)
	defer idx.Close()

	for i, host := range []string{"api.anthropic.com", "github.com", "api.anthropic.com"} {
		ev := types.StoredEvent{ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{ID: string(rune('a' + i)), URL: "https://" + host + "/"},
			Kind:    "generic",
		}}
		require.NoError(t, idx.Insert(ev, Location{}))
	}
	rows, err := idx.Query(QueryFilter{Host: "api.anthropic.com"})
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}
