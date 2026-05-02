package dismissals

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromMissingFileReturnsEmpty(t *testing.T) {
	d, err := Load("/nonexistent/path.json")
	require.NoError(t, err)
	assert.False(t, d.Has("evt-1", "host_not_allowlisted", "evil.com"))
}

func TestAddEventScope(t *testing.T) {
	d, err := Load(filepath.Join(t.TempDir(), "d.json"))
	require.NoError(t, err)
	require.NoError(t, d.Add(ScopeEvent, "evt-1", "secret_in_request", "", "tested in CI"))
	assert.True(t, d.Has("evt-1", "secret_in_request", "any.host"))
	assert.False(t, d.Has("evt-2", "secret_in_request", "any.host"),
		"event-scope dismissal must not apply to other events")
}

func TestAddHostCodeScope(t *testing.T) {
	d, err := Load(filepath.Join(t.TempDir(), "d.json"))
	require.NoError(t, err)
	require.NoError(t, d.Add(ScopeHostCode, "evt-1", "host_not_allowlisted", "metrics.example.com", "internal monitoring"))
	assert.True(t, d.Has("any-evt", "host_not_allowlisted", "metrics.example.com"))
	assert.False(t, d.Has("any-evt", "host_not_allowlisted", "different.example.com"))
}

func TestAddGlobalCodeScope(t *testing.T) {
	d, err := Load(filepath.Join(t.TempDir(), "d.json"))
	require.NoError(t, err)
	require.NoError(t, d.Add(ScopeGlobalCode, "evt-1", "permissive_capture", "", "I know I am not in airtight mode"))
	assert.True(t, d.Has("any-evt", "permissive_capture", "any.host"))
}

func TestPersistsAcrossLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	d, err := Load(path)
	require.NoError(t, err)
	require.NoError(t, d.Add(ScopeEvent, "evt-1", "code-x", "", "reason"))

	d2, err := Load(path)
	require.NoError(t, err)
	assert.True(t, d2.Has("evt-1", "code-x", ""))
}

func TestEntries(t *testing.T) {
	d, err := Load(filepath.Join(t.TempDir(), "d.json"))
	require.NoError(t, err)
	require.NoError(t, d.Add(ScopeEvent, "evt-1", "x", "", "r1"))
	require.NoError(t, d.Add(ScopeHostCode, "evt-2", "y", "h", "r2"))
	all := d.Entries()
	assert.Len(t, all, 2)
}

func TestRejectsUnknownScope(t *testing.T) {
	d, err := Load(filepath.Join(t.TempDir(), "d.json"))
	require.NoError(t, err)
	err = d.Add("not-a-scope", "evt", "code", "host", "reason")
	assert.Error(t, err)
}

func TestFileWrittenAt0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not honor unix file mode bits")
	}
	path := filepath.Join(t.TempDir(), "d.json")
	d, err := Load(path)
	require.NoError(t, err)
	require.NoError(t, d.Add(ScopeEvent, "e", "c", "", "r"))
	st, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), st.Mode().Perm())
}
