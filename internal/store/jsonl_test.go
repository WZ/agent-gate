package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONLAppendWritesLineAndReturnsOffset(t *testing.T) {
	dir := t.TempDir()
	w, err := NewJSONLWriter(dir, fixedClock(time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer w.Close()

	ev := types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow: types.RawFlow{ID: "01HX", Method: "GET"},
		Kind:    "generic",
	}}
	loc, err := w.Append(ev)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "2026-04-28.jsonl"), loc.Path)
	assert.Equal(t, int64(0), loc.Offset)
	assert.Greater(t, loc.Length, int64(0))

	// Second event has offset > first.
	ev.ID = "01HY"
	loc2, err := w.Append(ev)
	require.NoError(t, err)
	assert.Equal(t, loc.Length, loc2.Offset)
}

func TestJSONLRotatesOnDayBoundary(t *testing.T) {
	dir := t.TempDir()
	clock := newAdvancingClock(time.Date(2026, 4, 28, 23, 59, 59, 0, time.UTC))
	w, err := NewJSONLWriter(dir, clock.Now)
	require.NoError(t, err)
	defer w.Close()

	loc1, err := w.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{RawFlow: types.RawFlow{ID: "a"}}})
	require.NoError(t, err)
	clock.Advance(2 * time.Second)
	loc2, err := w.Append(types.StoredEvent{ParsedEvent: types.ParsedEvent{RawFlow: types.RawFlow{ID: "b"}}})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "2026-04-28.jsonl"), loc1.Path)
	assert.Equal(t, filepath.Join(dir, "2026-04-29.jsonl"), loc2.Path)
}

func TestJSONLReadAtOffset(t *testing.T) {
	dir := t.TempDir()
	w, err := NewJSONLWriter(dir, fixedClock(time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)))
	require.NoError(t, err)
	defer w.Close()

	ev := types.StoredEvent{ParsedEvent: types.ParsedEvent{
		RawFlow: types.RawFlow{ID: "01HX"}, Kind: "generic",
	}}
	loc, err := w.Append(ev)
	require.NoError(t, err)

	r, err := os.Open(loc.Path)
	require.NoError(t, err)
	defer r.Close()
	_, err = r.Seek(loc.Offset, 0)
	require.NoError(t, err)
	buf := make([]byte, loc.Length)
	_, err = r.Read(buf)
	require.NoError(t, err)
	line := strings.TrimSpace(string(buf))

	var decoded types.StoredEvent
	require.NoError(t, json.Unmarshal([]byte(line), &decoded))
	assert.Equal(t, "01HX", decoded.ID)
}

// helpers
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

type advancingClock struct{ t time.Time }

func newAdvancingClock(start time.Time) *advancingClock { return &advancingClock{t: start} }
func (c *advancingClock) Now() time.Time                { return c.t }
func (c *advancingClock) Advance(d time.Duration)       { c.t = c.t.Add(d) }
