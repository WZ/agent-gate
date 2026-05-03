package store

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedClock returns a clock that always returns t. Defined in store_test.go;
// referenced from the same package.

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
