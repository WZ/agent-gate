package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-gate/internal/config"
)

func TestDefaultConfigToml_HasComments(t *testing.T) {
	out := defaultConfigToml()
	wantSubstrings := []string{
		"# agent-gate config",
		`# "airtight" forces all subprocess egress`,
		"# Loopback-only",
		"# Where captured events are persisted",
		`# How often to rotate JSONL`,
		"# When true, the proxy returns 403",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(out, s) {
			t.Errorf("defaultConfigToml missing comment: %q", s)
		}
	}
	if strings.Contains(out, `file = "~/.config/agent-gate/allowlist.txt"`) {
		t.Error("defaultConfigToml still contains deprecated allowlist.file field")
	}
}

func TestDefaultConfigToml_Parses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(defaultConfigToml()), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := config.LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if cfg.Capture.DefaultMode != "airtight" {
		t.Errorf("DefaultMode: got %q, want airtight", cfg.Capture.DefaultMode)
	}
	if cfg.Ports.Proxy != 8888 {
		t.Errorf("Proxy port: got %d, want 8888", cfg.Ports.Proxy)
	}
}
