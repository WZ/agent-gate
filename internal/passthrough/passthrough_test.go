package passthrough

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPassthrough_Remove_Existing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passthrough.txt")
	p, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_ = p.Add("mcp-proxy.anthropic.com")
	_ = p.Add("pinned.example.com")
	if err := p.Remove("mcp-proxy.anthropic.com"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if p.Contains("mcp-proxy.anthropic.com") {
		t.Fatal("expected mcp-proxy.anthropic.com to be removed")
	}
	if !p.Contains("pinned.example.com") {
		t.Fatal("expected pinned.example.com to still be present")
	}
}

func TestPassthrough_Remove_NonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passthrough.txt")
	p, _ := Load(path)
	if err := p.Remove("never.there"); err != nil {
		t.Fatalf("Remove of missing host should be idempotent, got: %v", err)
	}
}

func TestPassthrough_Remove_PreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "passthrough.txt")
	p, _ := Load(path)
	_ = p.Add("a.example.com")
	_ = p.Add("b.example.com")
	_ = p.Remove("a.example.com")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected 0600, got %o", mode)
	}
}
