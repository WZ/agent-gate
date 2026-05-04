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

// FlagCode filter must do exact-item match against the comma-separated
// flag_codes column — never a naked LIKE that would let "host" match
// "host_not_allowlisted". Three rows: one with host_not_allowlisted,
// one with parse_error+host_not_allowlisted, one with parse_error
// alone.
func TestSQLiteFilterByFlagCode(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	idx, err := OpenIndex(dbPath)
	require.NoError(t, err)
	defer idx.Close()

	cases := []struct {
		id    string
		flags []types.Flag
	}{
		{"flag-a", []types.Flag{{Code: "host_not_allowlisted", Severity: "high"}}},
		{"flag-b", []types.Flag{
			{Code: "parse_error", Severity: "info"},
			{Code: "host_not_allowlisted", Severity: "high"},
		}},
		{"flag-c", []types.Flag{{Code: "parse_error", Severity: "info"}}},
	}
	for _, c := range cases {
		ev := types.StoredEvent{
			ParsedEvent: types.ParsedEvent{RawFlow: types.RawFlow{ID: c.id, URL: "https://api.anthropic.com/x"}},
			Flags:       c.flags,
		}
		require.NoError(t, idx.Insert(ev, Location{}))
	}

	hostMatch, err := idx.Query(QueryFilter{FlagCode: "host_not_allowlisted"})
	require.NoError(t, err)
	got := []string{}
	for _, r := range hostMatch {
		got = append(got, r.ID)
	}
	assert.ElementsMatch(t, []string{"flag-a", "flag-b"}, got)

	// "host" must NOT match "host_not_allowlisted" — exact-item only.
	prefix, err := idx.Query(QueryFilter{FlagCode: "host"})
	require.NoError(t, err)
	assert.Empty(t, prefix, "FlagCode=host must not match host_not_allowlisted")

	parse, err := idx.Query(QueryFilter{FlagCode: "parse_error"})
	require.NoError(t, err)
	got = got[:0]
	for _, r := range parse {
		got = append(got, r.ID)
	}
	assert.ElementsMatch(t, []string{"flag-b", "flag-c"}, got)

	missing, err := idx.Query(QueryFilter{FlagCode: "nonexistent_code"})
	require.NoError(t, err)
	assert.Empty(t, missing)
}

func TestSQLiteFilterByFlagCodeTreatsLikeMetacharactersLiterally(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	idx, err := OpenIndex(dbPath)
	require.NoError(t, err)
	defer idx.Close()

	cases := []struct {
		id   string
		code string
	}{
		{"literal-wildcards", "literal_%_flag"},
		{"wildcard-lookalike", "literal_AX_flag"},
		{"ordinary-flag", "parse_error"},
	}
	for _, c := range cases {
		ev := types.StoredEvent{
			ParsedEvent: types.ParsedEvent{RawFlow: types.RawFlow{ID: c.id, URL: "https://api.anthropic.com/x"}},
			Flags:       []types.Flag{{Code: c.code, Severity: "info"}},
		}
		require.NoError(t, idx.Insert(ev, Location{}))
	}

	rows, err := idx.Query(QueryFilter{FlagCode: "literal_%_flag"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "literal-wildcards", rows[0].ID)

	rows, err = idx.Query(QueryFilter{FlagCode: "%"})
	require.NoError(t, err)
	assert.Empty(t, rows, "FlagCode=% must be treated as a literal code, not a LIKE wildcard")
}

func TestInsertStripsPortFromHost(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	idx, err := OpenIndex(dbPath)
	require.NoError(t, err)
	defer idx.Close()

	ev := types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow: types.RawFlow{
			ID:     "port-test",
			Method: "POST",
			URL:    "https://api.anthropic.com:443/v1/messages",
		},
		Kind: "anthropic_messages",
	}}
	require.NoError(t, idx.Insert(ev, Location{}))

	// Query with no-port host — must find the row.
	rows, err := idx.Query(QueryFilter{Host: "api.anthropic.com"})
	require.NoError(t, err)
	require.Len(t, rows, 1, "event stored with :443 should be found by host without port")
	assert.Equal(t, "api.anthropic.com", rows[0].Host)
}
