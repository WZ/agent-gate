package allowlist

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFromMissingFileReturnsEmpty(t *testing.T) {
	a, err := Load("/nonexistent/path.txt")
	require.NoError(t, err)
	assert.False(t, a.Contains("api.anthropic.com"))
}

func TestLoadParsesHostsAndIgnoresCommentsAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")
	require.NoError(t, os.WriteFile(path, []byte(`
# Anthropic
api.anthropic.com

# GitHub
api.github.com   # trailing comments allowed
`), 0o600))

	a, err := Load(path)
	require.NoError(t, err)
	assert.True(t, a.Contains("api.anthropic.com"))
	assert.True(t, a.Contains("api.github.com"))
	assert.False(t, a.Contains("evil.example.com"))
}

func TestAddAppendsAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")
	require.NoError(t, os.WriteFile(path, []byte("api.anthropic.com\n"), 0o600))

	a, err := Load(path)
	require.NoError(t, err)
	require.NoError(t, a.Add("github.com"))

	assert.True(t, a.Contains("github.com"))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(string(data), "\n")
	assert.Contains(t, lines, "github.com")
}

func TestAddIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")
	a, err := Load(path)
	require.NoError(t, err)
	require.NoError(t, a.Add("foo.com"))
	require.NoError(t, a.Add("foo.com"))
	data, _ := os.ReadFile(path)
	assert.Equal(t, 1, strings.Count(string(data), "foo.com"))
}

func TestAddIsHostnameOnlyNoSchemeNoPort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")
	a, err := Load(path)
	require.NoError(t, err)
	err = a.Add("https://foo.com:443/")
	assert.Error(t, err, "should reject URLs / ports")
}

func TestAllowlist_Remove_Existing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")
	a, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := a.Add("api.anthropic.com"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := a.Add("api.openai.com"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := a.Remove("api.anthropic.com"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if a.Contains("api.anthropic.com") {
		t.Fatal("expected api.anthropic.com to be removed")
	}
	if !a.Contains("api.openai.com") {
		t.Fatal("expected api.openai.com to still be present")
	}
	b, err := Load(path)
	if err != nil {
		t.Fatalf("Load reload: %v", err)
	}
	if b.Contains("api.anthropic.com") {
		t.Fatal("expected reloaded allowlist to not contain api.anthropic.com")
	}
	if !b.Contains("api.openai.com") {
		t.Fatal("expected reloaded allowlist to contain api.openai.com")
	}
}

func TestAllowlist_Remove_NonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")
	a, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := a.Remove("never.there.example"); err != nil {
		t.Fatalf("Remove of missing host should be idempotent, got: %v", err)
	}
}

func TestAllowlist_Remove_PreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows doesn't honor unix file modes")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")
	a, _ := Load(path)
	_ = a.Add("api.anthropic.com")
	_ = a.Add("api.openai.com")
	_ = a.Remove("api.anthropic.com")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected 0600, got %o", mode)
	}
}
