package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfigDir_DefaultLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG defaults only meaningful on linux")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	want := filepath.Join(home, ".config", "agent-gate")
	if got != want {
		t.Fatalf("ConfigDir: got %q, want %q", got, want)
	}
}

func TestConfigDir_XDGOverride(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip()
	}
	override := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", override)
	got, err := ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	want := filepath.Join(override, "agent-gate")
	if got != want {
		t.Fatalf("ConfigDir: got %q, want %q", got, want)
	}
}

func TestDataDir_DefaultLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip()
	}
	t.Setenv("XDG_DATA_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := DataDir()
	if err != nil {
		t.Fatalf("DataDir: %v", err)
	}
	want := filepath.Join(home, ".local", "share", "agent-gate")
	if got != want {
		t.Fatalf("DataDir: got %q, want %q", got, want)
	}
}

func TestConfigPath(t *testing.T) {
	tmp := t.TempDir()
	if runtime.GOOS == "linux" {
		t.Setenv("XDG_CONFIG_HOME", tmp)
	} else {
		t.Setenv("HOME", tmp)
	}
	got, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if filepath.Base(got) != "config.toml" {
		t.Fatalf("ConfigPath: expected config.toml suffix, got %q", got)
	}
	if _, statErr := os.Stat(filepath.Dir(got)); statErr != nil && !os.IsNotExist(statErr) {
		t.Fatalf("parent dir stat: %v", statErr)
	}
}
