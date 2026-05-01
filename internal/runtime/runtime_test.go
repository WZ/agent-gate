package runtime_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"agent-gate/internal/runtime"
	"agent-gate/internal/store"
	"agent-gate/internal/types"
)

func TestLoadCommon_FreshConfig(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	must(t, os.WriteFile(configPath, []byte(`
[ports]
proxy = 8888
dashboard = 7878
[storage]
data_dir = "`+tmp+`/data"
`), 0o600))

	rt, err := runtime.LoadCommon(configPath)
	if err != nil {
		t.Fatalf("LoadCommon: %v", err)
	}
	defer rt.Close()
	if rt.CA == nil || rt.Store == nil || rt.Engine == nil {
		t.Fatalf("LoadCommon returned partial Common: %+v", rt)
	}
	if !rt.Allowlist.Contains("api.anthropic.com") {
		t.Errorf("expected api.anthropic.com seeded into allowlist")
	}
}

func TestRunPipeline_PersistsFlow(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	must(t, os.WriteFile(configPath, []byte(`
[ports]
proxy = 8888
dashboard = 7878
[storage]
data_dir = "`+tmp+`/data"
`), 0o600))

	rt, err := runtime.LoadCommon(configPath)
	if err != nil {
		t.Fatalf("LoadCommon: %v", err)
	}
	defer rt.Close()

	in := make(chan types.RawFlow, 1)
	in <- types.RawFlow{
		ID:          "01HZ1234TEST",
		StartedAt:   time.Now(),
		EndedAt:     time.Now(),
		Method:      "GET",
		URL:         "https://api.anthropic.com/v1/messages",
		RespStatus:  200,
		CaptureMode: "permissive",
	}
	close(in)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.RunPipeline(ctx, rt, in)
	}()
	<-done

	// One event in the store's index.
	got, err := rt.Store.Index().Query(store.QueryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 event, got %d", len(got))
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
