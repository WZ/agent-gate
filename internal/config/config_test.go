package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	c, err := LoadFromFile("/nonexistent/path.toml")
	require.NoError(t, err)
	assert.Equal(t, 8888, c.Ports.Proxy)
	assert.Equal(t, 7878, c.Ports.Dashboard)
	assert.Equal(t, "airtight", c.Capture.DefaultMode)
}

func TestLoadFromFileOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	require.NoError(t, os.WriteFile(path, []byte(`
[capture]
default_mode = "permissive"

[ports]
proxy = 9999
`), 0o600))

	c, err := LoadFromFile(path)
	require.NoError(t, err)
	assert.Equal(t, "permissive", c.Capture.DefaultMode)
	assert.Equal(t, 9999, c.Ports.Proxy)
	assert.Equal(t, 7878, c.Ports.Dashboard) // unspecified — default
}

func TestExpandsTilde(t *testing.T) {
	c := &Config{Storage: StorageConfig{DataDir: "~/foo"}}
	require.NoError(t, c.expandPaths())
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, "foo"), c.Storage.DataDir)
}

func TestLoadFromFile_IgnoresDeprecatedAllowlistFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	legacy := []byte(`
[capture]
default_mode = "airtight"

[ports]
proxy = 8888
dashboard = 7878

[storage]
data_dir = "~/.local/share/agent-gate"

[allowlist]
file = "/tmp/custom-allowlist.txt"
enforce = true
`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile should tolerate legacy field, got: %v", err)
	}
	if cfg.Allowlist.Enforce != true {
		t.Fatalf("expected Enforce=true, got %v", cfg.Allowlist.Enforce)
	}
}
