package store

import (
	"database/sql"
	"fmt"

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
