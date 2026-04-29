package policy

import (
	"path/filepath"
	"testing"

	"agent-gate/internal/allowlist"
	"agent-gate/internal/dismissals"
	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkengine(t *testing.T, rules ...Rule) (*Engine, *allowlist.Allowlist) {
	t.Helper()
	dir := t.TempDir()
	al, err := allowlist.Load(filepath.Join(dir, "a.txt"))
	require.NoError(t, err)
	di, err := dismissals.Load(filepath.Join(dir, "d.json"))
	require.NoError(t, err)
	return NewEngine(al, di, rules...), al
}

func TestHostNotAllowlistedFiresOnUnknownHost(t *testing.T) {
	dir := t.TempDir()
	al, _ := allowlist.Load(filepath.Join(dir, "a.txt"))
	di, _ := dismissals.Load(filepath.Join(dir, "d.json"))
	require.NoError(t, al.Add("api.anthropic.com"))
	e := NewEngine(al, di, NewHostNotAllowlistedRule(al))

	ev := &types.ParsedEvent{RawFlow: types.RawFlow{ID: "e1", URL: "https://evil.example.com/foo"}}
	flags := e.Evaluate(ev)
	require.Len(t, flags, 1)
	assert.Equal(t, "host_not_allowlisted", flags[0].Code)
	assert.Equal(t, "high", flags[0].Severity)
	assert.Contains(t, flags[0].Detail, "evil.example.com")
}

func TestHostNotAllowlistedSilentForKnownHost(t *testing.T) {
	dir := t.TempDir()
	al, _ := allowlist.Load(filepath.Join(dir, "a.txt"))
	di, _ := dismissals.Load(filepath.Join(dir, "d.json"))
	require.NoError(t, al.Add("api.anthropic.com"))
	e := NewEngine(al, di, NewHostNotAllowlistedRule(al))

	ev := &types.ParsedEvent{RawFlow: types.RawFlow{ID: "e2", URL: "https://api.anthropic.com/v1/messages"}}
	flags := e.Evaluate(ev)
	assert.Empty(t, flags)
}

func TestPermissiveCaptureFiresWhenModeIsPermissive(t *testing.T) {
	e, _ := mkengine(t, PermissiveCaptureRule{})
	flags := e.Evaluate(&types.ParsedEvent{RawFlow: types.RawFlow{ID: "e", CaptureMode: "permissive"}})
	require.Len(t, flags, 1)
	assert.Equal(t, "permissive_capture", flags[0].Code)
	assert.Equal(t, "info", flags[0].Severity)
}

func TestPermissiveCaptureSilentInAirtight(t *testing.T) {
	e, _ := mkengine(t, PermissiveCaptureRule{})
	flags := e.Evaluate(&types.ParsedEvent{RawFlow: types.RawFlow{ID: "e", CaptureMode: "airtight"}})
	assert.Empty(t, flags)
}
