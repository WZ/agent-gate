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
data_dir = '`+filepath.ToSlash(tmp)+`/data'
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
data_dir = '`+filepath.ToSlash(tmp)+`/data'
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

	ctx := t.Context()
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

// TestRunPipeline_DrainsOnCancel verifies that cancelling the context does not
// lose buffered flows: RunPipeline must drain the channel before returning.
func TestRunPipeline_DrainsOnCancel(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	must(t, os.WriteFile(configPath, []byte(`
[ports]
proxy = 8888
dashboard = 7878
[storage]
data_dir = '`+filepath.ToSlash(tmp)+`/data'
`), 0o600))

	rt, err := runtime.LoadCommon(configPath)
	if err != nil {
		t.Fatalf("LoadCommon: %v", err)
	}
	defer rt.Close()

	// Pre-fill a buffered channel with 2 flows before starting the pipeline.
	in := make(chan types.RawFlow, 2)
	for _, id := range []string{"01HZ0000DRAIN1", "01HZ0000DRAIN2"} {
		in <- types.RawFlow{
			ID:          id,
			StartedAt:   time.Now(),
			EndedAt:     time.Now(),
			Method:      "GET",
			URL:         "https://api.anthropic.com/v1/messages",
			RespStatus:  200,
			CaptureMode: "permissive",
		}
	}

	// Cancel ctx before closing the channel so RunPipeline takes the drain
	// path (ctx.Done branch) rather than the clean channel-close path.
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancelled immediately; drain path must still persist both flows

	done := make(chan struct{})
	go func() {
		defer close(done)
		runtime.RunPipeline(ctx, rt, in)
	}()
	close(in) // unblock the drain loop
	<-done

	got, err := rt.Store.Index().Query(store.QueryFilter{Limit: 10})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 events after drain, got %d", len(got))
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
