package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"agent-gate/internal/types"

	"github.com/klauspost/compress/zstd"
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
		`SELECT side, code, count FROM event_pii WHERE event_id = ? AND count > 0 ORDER BY side, code`, "01EVT1")
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

func TestIndexPIIDecodesZstdRequestBody(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	ev := types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:         "01EVTZSTD",
				URL:        "https://chatgpt.com/backend-api/codex/responses",
				Method:     "POST",
				ReqHeaders: http.Header{"Content-Type": []string{"application/json"}, "Content-Encoding": []string{"zstd"}},
				ReqBody:    zstdEncodeForTest(t, []byte(`{"input":"email alice@example.com"}`)),
			},
			Kind: "openai_responses",
		},
	}

	require.NoError(t, s.indexPII(ev))

	row := s.Index().db.QueryRow(
		`SELECT count FROM event_pii WHERE event_id = ? AND side = 'req' AND code = 'email'`, "01EVTZSTD")
	var n int
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, 1, n)
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
	row = s.Index().db.QueryRow(`SELECT count(*) FROM event_pii WHERE count > 0`)
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

func TestMaybeReindexTriggersWhenTableIsBehind(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	// Append two events with PII.
	for _, id := range []string{"01A", "01B"} {
		require.NoError(t, s.Append(types.StoredEvent{
			ParsedEvent: types.ParsedEvent{
				RawFlow: types.RawFlow{
					ID:         id,
					URL:        "https://api.example.com/x",
					Method:     "POST",
					ReqHeaders: http.Header{"Content-Type": []string{"application/json"}},
					ReqBody:    []byte(`{"email":"a@b.co"}`),
				},
				Kind: "generic",
			},
		}))
	}
	// Wipe event_pii to simulate a freshly-upgraded DB.
	_, err = s.Index().db.Exec(`DELETE FROM event_pii`)
	require.NoError(t, err)

	ran, err := s.MaybeReindexPII(context.Background())
	require.NoError(t, err)
	assert.True(t, ran, "MaybeReindexPII should fire when event_pii is behind")

	row := s.Index().db.QueryRow(`SELECT count(*) FROM event_pii WHERE count > 0`)
	var n int
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, 2, n)
}

func TestMaybeReindexSkipsWhenCaughtUp(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.Append(types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:         "01CAUGHT",
				URL:        "https://api.example.com/x",
				Method:     "POST",
				ReqHeaders: http.Header{"Content-Type": []string{"application/json"}},
				ReqBody:    []byte(`{"email":"a@b.co"}`),
			},
			Kind: "generic",
		},
	}))

	// event_pii already has the row from Append.
	ran, err := s.MaybeReindexPII(context.Background())
	require.NoError(t, err)
	assert.False(t, ran, "no work to do; should skip")
}

func TestMaybeReindexSkipsWhenZeroPIIEventsAreCaughtUp(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.Append(types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:         "01PLAIN",
				URL:        "https://api.example.com/x",
				Method:     "POST",
				ReqHeaders: http.Header{"Content-Type": []string{"application/json"}},
				ReqBody:    []byte(`{"plain":"text"}`),
			},
			Kind: "generic",
		},
	}))

	ran, err := s.MaybeReindexPII(context.Background())
	require.NoError(t, err)
	assert.False(t, ran, "zero-PII events should still be marked as indexed")
}

func TestMaybeReindexTriggersWhenWebSocketColumnsWereMigrated(t *testing.T) {
	dir := t.TempDir()
	parentID := "upgrade-parent"
	messageType := "text"
	direction := "s2c"
	controlOp := "pong"
	closeCode := 1001
	ev := types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow: types.RawFlow{
			ID:          "01WSMIGRATE",
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
	w, err := NewJSONLWriter(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	loc, err := w.Append(ev)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	createOldIndexWithPIIMarker(t, filepath.Join(dir, "events.db"), ev, loc)

	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	ran, err := s.MaybeReindex(context.Background())
	require.NoError(t, err)
	assert.True(t, ran, "schema migration should trigger metadata reindex even when event_pii is caught up")

	row, err := s.Index().QueryByID("01WSMIGRATE")
	require.NoError(t, err)
	require.NotNil(t, row.ParentID)
	assert.Equal(t, parentID, *row.ParentID)
	require.NotNil(t, row.MessageType)
	assert.Equal(t, messageType, *row.MessageType)
	require.NotNil(t, row.Direction)
	assert.Equal(t, direction, *row.Direction)
	assert.True(t, row.IsWSMessage)
	require.NotNil(t, row.ControlOp)
	assert.Equal(t, controlOp, *row.ControlOp)
	require.NotNil(t, row.CloseCode)
	assert.Equal(t, closeCode, *row.CloseCode)
}

func TestIndexPIIRemovesStaleRowsForEvent(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, fixedClock(time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer s.Close()

	ev := types.StoredEvent{
		ParsedEvent: types.ParsedEvent{
			RawFlow: types.RawFlow{
				ID:         "01STALE",
				URL:        "https://api.example.com/x",
				Method:     "POST",
				ReqHeaders: http.Header{"Content-Type": []string{"application/json"}},
				ReqBody:    []byte(`{"email":"a@b.co"}`),
			},
			Kind: "generic",
		},
	}
	require.NoError(t, s.indexPII(ev))

	ev.ReqBody = []byte(`{"plain":"text"}`)
	require.NoError(t, s.indexPII(ev))

	row := s.Index().db.QueryRow(
		`SELECT count(*) FROM event_pii WHERE event_id = ? AND count > 0`, "01STALE")
	var n int
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, 0, n, "stale positive PII rows should be removed before reindexing an event")
}

func createOldIndexWithPIIMarker(t *testing.T, dbPath string, ev types.StoredEvent, loc Location) {
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
CREATE TABLE event_pii (
	event_id   TEXT NOT NULL,
	side       TEXT NOT NULL CHECK(side IN ('req','resp')),
	code       TEXT NOT NULL,
	count      INTEGER NOT NULL,
	PRIMARY KEY (event_id, side, code)
);
CREATE INDEX idx_event_pii_code ON event_pii(code, event_id);
INSERT INTO events (
	id, started_at, ended_at, host, method, path, status, kind,
	session_id, model, input_tokens, output_tokens, cache_read,
	capture_mode, flag_codes, flags_json, jsonl_path, jsonl_offset, jsonl_length
) VALUES (?, 0, 0, 'chatgpt.com', '', '/backend-api/codex/session', 0, ?,
	'', '', 0, 0, 0, '', '', '[]', ?, ?, ?);
INSERT INTO event_pii(event_id, side, code, count)
VALUES (?, 'req', ?, 0);`,
		ev.ID,
		ev.Kind,
		loc.Path,
		loc.Offset,
		loc.Length,
		ev.ID,
		piiIndexedMarkerCode,
	)
	require.NoError(t, err)
}

func zstdEncodeForTest(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	require.NoError(t, err)
	_, err = enc.Write(body)
	require.NoError(t, err)
	require.NoError(t, enc.Close())
	return buf.Bytes()
}
