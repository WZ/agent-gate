package store

import (
	"database/sql"
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

func TestSQLiteWebSocketFieldsRoundTripAndOldSchemaMigration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "events.db")
	createOldEventsSchema(t, dbPath)

	idx, err := OpenIndex(dbPath)
	require.NoError(t, err)
	defer idx.Close()

	assertEventsTableColumns(t, idx, []string{
		"parent_id",
		"message_type",
		"direction",
		"is_ws_message",
		"control_op",
		"close_code",
	})
	assertIndexExists(t, idx, "idx_events_parent_id")

	var oldParentID sql.NullString
	var oldMessageType sql.NullString
	var oldDirection sql.NullString
	var oldIsWSMessage int
	var oldControlOp sql.NullString
	var oldCloseCode sql.NullInt64
	err = idx.Db().QueryRow(`
SELECT parent_id, message_type, direction, is_ws_message, control_op, close_code
FROM events WHERE id = ?`, "old-event").Scan(
		&oldParentID,
		&oldMessageType,
		&oldDirection,
		&oldIsWSMessage,
		&oldControlOp,
		&oldCloseCode,
	)
	require.NoError(t, err)
	assert.False(t, oldParentID.Valid)
	assert.False(t, oldMessageType.Valid)
	assert.False(t, oldDirection.Valid)
	assert.Zero(t, oldIsWSMessage)
	assert.False(t, oldControlOp.Valid)
	assert.False(t, oldCloseCode.Valid)

	parentID := "parent-upgrade"
	messageType := "text"
	direction := "s2c"
	controlOp := "pong"
	closeCode := 1001
	ev := types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow: types.RawFlow{
			ID:          "ws-child",
			URL:         "https://chatgpt.com/backend-api/codex/session",
			ParentID:    &parentID,
			MessageType: &messageType,
			Direction:   &direction,
			IsWSMessage: true,
			ControlOp:   &controlOp,
			CloseCode:   &closeCode,
		},
		Kind: "chatgpt_realtime",
	}}
	require.NoError(t, idx.Insert(ev, Location{Path: "/tmp/events.jsonl", Offset: 10, Length: 99}))

	rows, err := idx.Query(QueryFilter{Host: "chatgpt.com"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].ParentID)
	assert.Equal(t, parentID, *rows[0].ParentID)
	require.NotNil(t, rows[0].MessageType)
	assert.Equal(t, messageType, *rows[0].MessageType)
	require.NotNil(t, rows[0].Direction)
	assert.Equal(t, direction, *rows[0].Direction)
	assert.True(t, rows[0].IsWSMessage)
	require.NotNil(t, rows[0].ControlOp)
	assert.Equal(t, controlOp, *rows[0].ControlOp)
	require.NotNil(t, rows[0].CloseCode)
	assert.Equal(t, closeCode, *rows[0].CloseCode)

	require.NoError(t, idx.Insert(types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow: types.RawFlow{
			ID:  "plain-http",
			URL: "https://plain.example.com/",
		},
		Kind: "generic",
	}}, Location{}))

	rows, err = idx.Query(QueryFilter{Host: "plain.example.com"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Nil(t, rows[0].ParentID)
	assert.Nil(t, rows[0].MessageType)
	assert.Nil(t, rows[0].Direction)
	assert.False(t, rows[0].IsWSMessage)
	assert.Nil(t, rows[0].ControlOp)
	assert.Nil(t, rows[0].CloseCode)
}

func createOldEventsSchema(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
CREATE TABLE events (
	id            TEXT PRIMARY KEY,
	started_at    INTEGER,
	ended_at      INTEGER,
	host          TEXT,
	method        TEXT,
	path          TEXT,
	status        INTEGER,
	kind          TEXT,
	session_id    TEXT,
	model         TEXT,
	input_tokens  INTEGER,
	output_tokens INTEGER,
	cache_read    INTEGER,
	capture_mode  TEXT,
	flag_codes    TEXT,
	flags_json    TEXT,
	jsonl_path    TEXT,
	jsonl_offset  INTEGER,
	jsonl_length  INTEGER
);
INSERT INTO events (
	id, started_at, ended_at, host, method, path, status, kind,
	session_id, model, input_tokens, output_tokens, cache_read,
	capture_mode, flag_codes, flags_json, jsonl_path, jsonl_offset, jsonl_length
) VALUES (
	'old-event', 0, 0, 'example.com', 'GET', '/', 200, 'generic',
	'', '', 0, 0, 0, '', '', '[]', '/tmp/old.jsonl', 0, 1
);`)
	require.NoError(t, err)
}

func assertEventsTableColumns(t *testing.T, idx *Index, names []string) {
	t.Helper()
	rows, err := idx.Db().Query(`PRAGMA table_info(events)`)
	require.NoError(t, err)
	defer rows.Close()

	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name string
		var typ string
		var notNull int
		var defaultValue any
		var pk int
		require.NoError(t, rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk))
		have[name] = true
	}
	require.NoError(t, rows.Err())

	for _, name := range names {
		assert.True(t, have[name], "expected events.%s column", name)
	}
}

func assertIndexExists(t *testing.T, idx *Index, name string) {
	t.Helper()
	var found int
	err := idx.Db().QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, name).Scan(&found)
	require.NoError(t, err)
	assert.Equal(t, 1, found)
}
