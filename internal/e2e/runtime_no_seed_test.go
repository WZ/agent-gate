package e2e

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agruntime "agent-gate/internal/runtime"
	"agent-gate/internal/store"
	"agent-gate/internal/types"
)

func TestRuntimeDoesNotAutoSeedAllowlist(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	// Use forward slashes in the data_dir literal so Windows tempdirs (C:\Users\...)
	// don't trigger TOML's unicode-escape parser on `\U`. filepath functions accept
	// either separator on Windows.
	dataDir := filepath.ToSlash(filepath.Join(dir, "data"))
	if err := os.WriteFile(cfgPath, []byte(`
[capture]
default_mode = "permissive"
[ports]
proxy = 18888
dashboard = 17878
[storage]
data_dir = "`+dataDir+`"
[allowlist]
enforce = false
`), 0o600); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	allowPath := filepath.Join(dir, "allowlist.txt")
	if err := os.WriteFile(allowPath, []byte(""), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}

	common, err := agruntime.LoadCommon(cfgPath)
	if err != nil {
		t.Fatalf("LoadCommon: %v", err)
	}
	defer common.Close()

	if common.Allowlist.Contains("api.anthropic.com") {
		t.Fatal("auto-seed regression: allowlist contains api.anthropic.com after LoadCommon")
	}

	flow := types.RawFlow{
		ID:          "test-1",
		Method:      "POST",
		URL:         "https://api.anthropic.com/v1/messages",
		ReqHeaders:  http.Header{"Host": []string{"api.anthropic.com"}},
		StartedAt:   time.Now(),
		EndedAt:     time.Now(),
		RespStatus:  200,
		CaptureMode: "permissive",
	}
	ch := make(chan types.RawFlow, 1)
	ch <- flow
	close(ch)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	agruntime.RunPipeline(ctx, common, ch)

	idx := common.Store.Index()
	rows, err := idx.Query(store.QueryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("no events persisted; pipeline drain failed")
	}
	flagsStr := rows[0].FlagCodes
	if !strings.Contains(flagsStr, "host_not_allowlisted") {
		t.Fatalf("expected host_not_allowlisted flag (auto-seed should be gone), got flags=%q", flagsStr)
	}

	contents, _ := os.ReadFile(allowPath)
	if strings.Contains(string(contents), "api.anthropic.com") {
		t.Fatalf("allowlist file mutated by runtime: %q", string(contents))
	}
}
