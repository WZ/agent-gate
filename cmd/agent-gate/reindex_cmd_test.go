package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"agent-gate/internal/store"
	"agent-gate/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReindexCommandRebuildsIndex builds the binary, points it at a
// temp data dir + minimal config, wipes event_pii, and verifies that
// `agent-gate reindex` repopulates the table. End-to-end shaped because
// reindex_cmd integrates several layers (cobra wiring, config load,
// store.Open, Reindex).
func TestReindexCommandRebuildsIndex(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary; skip in -short")
	}

	tmp := t.TempDir()
	binPath := filepath.Join(tmp, "agent-gate")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	build := exec.Command("go", "build", "-o", binPath, "./cmd/agent-gate")
	build.Dir = projectRoot(t)
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build: %s", out)

	dataDir := filepath.Join(tmp, "data")
	configDir := filepath.Join(tmp, "config")
	require.NoError(t, os.MkdirAll(dataDir, 0o700))
	require.NoError(t, os.MkdirAll(configDir, 0o700))
	configPath := filepath.Join(configDir, "config.toml")
	// Use TOML literal strings (single-quote) so Windows paths with
	// backslashes don't get interpreted as escape sequences.
	require.NoError(t, os.WriteFile(configPath, []byte(
		"[storage]\ndata_dir = '"+dataDir+"'\n[ports]\ndashboard = 0\nproxy = 0\n"), 0o600))

	// Seed two events with PII via the store directly.
	st, err := store.Open(dataDir, time.Now)
	require.NoError(t, err)
	for _, id := range []string{"01R1", "01R2"} {
		require.NoError(t, st.Append(types.StoredEvent{
			ParsedEvent: types.ParsedEvent{
				RawFlow: types.RawFlow{
					ID:         id,
					URL:        "https://api.example.com/x",
					Method:     "POST",
					ReqHeaders: http.Header{"Content-Type": []string{"application/json"}},
					ReqBody:    []byte(`{"email":"a@b.co"}`),
				},
				Kind: "generic",
			},
		}))
	}
	// Wipe event_pii to make reindex meaningful.
	_, err = st.Index().Exec("DELETE FROM event_pii")
	require.NoError(t, err)
	require.NoError(t, st.Close())

	cmd := exec.Command(binPath, "reindex", "--config", configPath)
	cmdOut, err := cmd.CombinedOutput()
	require.NoError(t, err, "reindex output: %s", cmdOut)
	assert.True(t,
		strings.Contains(string(cmdOut), "reindex complete") ||
			strings.Contains(string(cmdOut), "indexed 2 events"),
		"reindex should report progress; got %q", string(cmdOut))

	// Verify the index is repopulated.
	st2, err := store.Open(dataDir, time.Now)
	require.NoError(t, err)
	defer st2.Close()
	row := st2.Index().Db().QueryRow(`SELECT count(*) FROM event_pii WHERE count > 0`)
	var n int
	require.NoError(t, row.Scan(&n))
	assert.Equal(t, 2, n)
}

// projectRoot walks upward from the test cwd until it finds a go.mod.
func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("go.mod not found above test cwd")
	return ""
}
