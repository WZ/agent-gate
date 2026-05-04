package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"agent-gate/internal/types"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS events (
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
CREATE INDEX IF NOT EXISTS idx_session ON events(session_id, started_at);
CREATE INDEX IF NOT EXISTS idx_started ON events(started_at);
CREATE INDEX IF NOT EXISTS idx_host    ON events(host);

CREATE TABLE IF NOT EXISTS event_pii (
	event_id   TEXT NOT NULL,
	side       TEXT NOT NULL CHECK(side IN ('req','resp')),
	code       TEXT NOT NULL,
	count      INTEGER NOT NULL,
	PRIMARY KEY (event_id, side, code)
);
CREATE INDEX IF NOT EXISTS idx_event_pii_code ON event_pii(code, event_id);
`

// Index is a SQLite-backed event index.
type Index struct {
	db *sql.DB
}

// IndexRow is a column-projection of one event for list/search.
type IndexRow struct {
	ID           string
	StartedAt    time.Time
	Host         string
	Method       string
	Path         string
	Status       int
	Kind         string
	SessionID    string
	Model        string
	InputTokens  int
	OutputTokens int
	CacheRead    int
	CaptureMode  string
	FlagCodes    string
	JSONLPath    string
	JSONLOffset  int64
	JSONLLength  int64
}

// QueryFilter narrows a Query.
type QueryFilter struct {
	SessionID string
	Host      string
	Since     time.Time
	Until     time.Time
	Limit     int
}

func OpenIndex(path string) (*Index, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Index{db: db}, nil
}

func (i *Index) Close() error { return i.db.Close() }

// Truncate removes all rows from the events table and reclaims disk space.
func (i *Index) Truncate() error {
	if _, err := i.db.Exec("DELETE FROM events"); err != nil {
		return err
	}
	if _, err := i.db.Exec("DELETE FROM event_pii"); err != nil {
		return err
	}
	if _, err := i.db.Exec("VACUUM"); err != nil {
		return err
	}
	return nil
}

func (i *Index) Insert(ev types.StoredEvent, loc Location) error {
	host, path, err := splitURL(ev.URL)
	if err != nil {
		return err
	}
	codes := flagCodes(ev.Flags)
	flagsJSON, err := json.Marshal(ev.Flags)
	if err != nil {
		return err
	}
	_, err = i.db.Exec(`
INSERT INTO events (
	id, started_at, ended_at, host, method, path, status, kind,
	session_id, model, input_tokens, output_tokens, cache_read,
	capture_mode, flag_codes, flags_json, jsonl_path, jsonl_offset, jsonl_length
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		ev.ID,
		ev.StartedAt.UnixMilli(),
		ev.EndedAt.UnixMilli(),
		host,
		ev.Method,
		path,
		ev.RespStatus,
		ev.Kind,
		ev.SessionID,
		ev.Model,
		ev.Usage.InputTokens,
		ev.Usage.OutputTokens,
		ev.Usage.CacheRead,
		ev.CaptureMode,
		codes,
		string(flagsJSON),
		loc.Path,
		loc.Offset,
		loc.Length,
	)
	return err
}

func (i *Index) Query(f QueryFilter) ([]IndexRow, error) {
	var (
		conds []string
		args  []any
	)
	if f.SessionID != "" {
		conds = append(conds, "session_id = ?")
		args = append(args, f.SessionID)
	}
	if f.Host != "" {
		conds = append(conds, "host = ?")
		args = append(args, f.Host)
	}
	if !f.Since.IsZero() {
		conds = append(conds, "started_at >= ?")
		args = append(args, f.Since.UnixMilli())
	}
	if !f.Until.IsZero() {
		conds = append(conds, "started_at <= ?")
		args = append(args, f.Until.UnixMilli())
	}
	q := `SELECT id, started_at, host, method, path, status, kind, session_id, model,
		input_tokens, output_tokens, cache_read, capture_mode, flag_codes,
		jsonl_path, jsonl_offset, jsonl_length FROM events`
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY started_at DESC"
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}
	rows, err := i.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []IndexRow
	for rows.Next() {
		var r IndexRow
		var startedMs int64
		if err := rows.Scan(
			&r.ID, &startedMs, &r.Host, &r.Method, &r.Path, &r.Status, &r.Kind,
			&r.SessionID, &r.Model, &r.InputTokens, &r.OutputTokens, &r.CacheRead,
			&r.CaptureMode, &r.FlagCodes,
			&r.JSONLPath, &r.JSONLOffset, &r.JSONLLength,
		); err != nil {
			return nil, err
		}
		r.StartedAt = time.UnixMilli(startedMs).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

func splitURL(rawURL string) (host, path string, err error) {
	if rawURL == "" {
		return "", "", nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("parse url %q: %w", rawURL, err)
	}
	if u.Host == "" {
		return "", "", errors.New("url has no host")
	}
	return u.Hostname(), u.Path, nil
}

func (i *Index) QueryByID(id string) (IndexRow, error) {
	row := i.db.QueryRow(`SELECT id, started_at, host, method, path, status, kind, session_id, model,
		input_tokens, output_tokens, cache_read, capture_mode, flag_codes,
		jsonl_path, jsonl_offset, jsonl_length FROM events WHERE id = ?`, id)
	var r IndexRow
	var startedMs int64
	if err := row.Scan(
		&r.ID, &startedMs, &r.Host, &r.Method, &r.Path, &r.Status, &r.Kind,
		&r.SessionID, &r.Model, &r.InputTokens, &r.OutputTokens, &r.CacheRead,
		&r.CaptureMode, &r.FlagCodes,
		&r.JSONLPath, &r.JSONLOffset, &r.JSONLLength,
	); err != nil {
		return IndexRow{}, err
	}
	r.StartedAt = time.UnixMilli(startedMs).UTC()
	return r, nil
}

func flagCodes(flags []types.Flag) string {
	codes := make([]string, len(flags))
	for i, f := range flags {
		codes[i] = f.Code
	}
	return strings.Join(codes, ",")
}
