package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"agent-gate/internal/pii"
	"agent-gate/internal/types"
)

// indexPII writes per-side, per-code counts into event_pii for one event.
// Existing rows for (event_id, side, code) are replaced — callers may invoke
// it repeatedly during reindex without producing duplicates.
//
// Best-effort by design: the caller (Store.Append) logs and discards any
// error so a PII-index hiccup never aborts the audit-log write.
func (s *Store) indexPII(ev types.StoredEvent) error {
	reqMatches := pii.Find(ev.ReqBody, pii.DetectKind(ev.ReqHeaders))
	respMatches := pii.Find(ev.RespBody, pii.DetectKind(ev.RespHeaders))

	tx, err := s.idx.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := writePIIBucket(tx, ev.ID, "req", pii.CountByCode(reqMatches)); err != nil {
		return err
	}
	if err := writePIIBucket(tx, ev.ID, "resp", pii.CountByCode(respMatches)); err != nil {
		return err
	}
	return tx.Commit()
}

func writePIIBucket(tx *sql.Tx, eventID, side string, counts map[string]int) error {
	for code, n := range counts {
		if n == 0 {
			continue
		}
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO event_pii(event_id, side, code, count) VALUES (?, ?, ?, ?)`,
			eventID, side, code, n,
		); err != nil {
			return fmt.Errorf("insert event_pii %s/%s/%s: %w", eventID, side, code, err)
		}
	}
	return nil
}

// ReindexPII walks every event in the index, opens its JSONL slice, decodes
// the StoredEvent, runs pii.Find on its bodies, and writes counts back into
// event_pii via INSERT OR REPLACE. Safe to invoke while Append is running
// concurrently — INSERT OR REPLACE makes per-event updates atomic and the
// reindex iterates a snapshot of event ids.
//
// Cancelling ctx aborts cleanly between events.
func (s *Store) ReindexPII(ctx context.Context) error {
	rows, err := s.idx.Query(QueryFilter{Limit: 0})
	if err != nil {
		return fmt.Errorf("list events: %w", err)
	}
	for _, r := range rows {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		ev, err := s.loadStoredEvent(r.ID)
		if err != nil {
			// One unreadable event must not abort the entire reindex.
			log.Printf("store: reindex skipped %s (load): %v", r.ID, err)
			continue
		}
		if err := s.indexPII(ev); err != nil {
			// Same: skip and move on.
			log.Printf("store: reindex skipped %s (index): %v", r.ID, err)
			continue
		}
	}
	return nil
}

// MaybeReindexPII compares the count of distinct event_ids in event_pii
// against the events table. If events has more — typically because an
// indexPII call from Append failed and was swallowed (audit-log
// completeness rule), or because of a schema upgrade or aborted reindex —
// it runs ReindexPII and returns (true, nil). Otherwise returns (false,
// nil) without scanning bodies.
//
// Safe to invoke while Append is running; the count probe sees a snapshot
// and any drift it observes is resolved by ReindexPII's per-event INSERT
// OR REPLACE.
//
// Relies on Store.Clear truncating event_pii so the "behind never ahead"
// invariant holds — see Index.Truncate.
func (s *Store) MaybeReindexPII(ctx context.Context) (bool, error) {
	var eventCount int
	if err := s.idx.db.QueryRow(`SELECT count(*) FROM events`).Scan(&eventCount); err != nil {
		return false, fmt.Errorf("count events: %w", err)
	}
	var indexedCount int
	if err := s.idx.db.QueryRow(`SELECT count(DISTINCT event_id) FROM event_pii`).Scan(&indexedCount); err != nil {
		return false, fmt.Errorf("count event_pii: %w", err)
	}
	if eventCount <= indexedCount {
		return false, nil
	}
	return true, s.ReindexPII(ctx)
}

func (s *Store) loadStoredEvent(id string) (types.StoredEvent, error) {
	r, err := s.Body(id)
	if err != nil {
		return types.StoredEvent{}, err
	}
	defer r.Close()
	raw, err := io.ReadAll(r)
	if err != nil {
		return types.StoredEvent{}, err
	}
	var ev types.StoredEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return types.StoredEvent{}, err
	}
	return ev, nil
}
