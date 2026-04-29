package allowlist

import (
	"os"
	"path/filepath"
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
