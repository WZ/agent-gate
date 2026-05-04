package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-gate/internal/config"
)

func TestInitCmd_NonInteractive_WritesArtifacts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	cmd := initCmd()
	cmd.SetArgs([]string{
		"--config", cfg,
		"--non-interactive",
		"--install-cert=false",
		"--allow-host", "api.anthropic.com",
		"--allow-host", "api.openai.com",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("config.toml not written: %v", err)
	}
	allow, _ := os.ReadFile(filepath.Join(tmp, "allowlist.txt"))
	for _, want := range []string{"api.anthropic.com", "api.openai.com"} {
		if !strings.Contains(string(allow), want) {
			t.Errorf("allowlist missing %q; got %q", want, string(allow))
		}
	}
}

func TestInitCmd_RefusesExistingWithoutForce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	_ = os.WriteFile(cfg, []byte("existing"), 0o600)
	cmd := initCmd()
	cmd.SetArgs([]string{
		"--config", cfg,
		"--non-interactive",
		"--install-cert=false",
	})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error on existing config without --force")
	}
}

func TestInitCmd_PrintConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	cmd := initCmd()
	cmd.SetArgs([]string{
		"--config", cfg,
		"--print-config",
		"--non-interactive",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := os.Stat(cfg); err == nil {
		t.Errorf("--print-config should not write config.toml on disk")
	}
}

func TestInitCmd_RenderedTOMLParses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	cmd := initCmd()
	cmd.SetArgs([]string{
		"--config", cfg,
		"--non-interactive",
		"--install-cert=false",
		"--allow-host", "api.anthropic.com",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	loaded, err := config.LoadFromFile(cfg)
	if err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if loaded.Capture.DefaultMode != "airtight" {
		t.Errorf("DefaultMode: got %q, want airtight", loaded.Capture.DefaultMode)
	}
	if loaded.Ports.Proxy != 8888 {
		t.Errorf("Proxy port: got %d, want 8888", loaded.Ports.Proxy)
	}
}

func TestInitCmd_HasComments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")
	cmd := initCmd()
	cmd.SetArgs([]string{
		"--config", cfg,
		"--non-interactive",
		"--install-cert=false",
		"--allow-host", "api.anthropic.com",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	wantSubstrings := []string{
		"# agent-gate config",
		`# "airtight" forces all subprocess egress`,
		"# Loopback-only",
		"# When true, the proxy returns 403",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(string(out), s) {
			t.Errorf("config.toml missing comment: %q", s)
		}
	}
	if strings.Contains(string(out), `file = "~/.config/agent-gate/allowlist.txt"`) {
		t.Error("config.toml still contains deprecated allowlist.file field")
	}
}
