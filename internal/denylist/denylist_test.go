package denylist

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDenylist_Remove_Existing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "denylist.txt")
	d, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_ = d.Add("evil.example.com")
	_ = d.Add("scary.example.com")
	if err := d.Remove("evil.example.com"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if d.Contains("evil.example.com") {
		t.Fatal("expected evil.example.com to be removed")
	}
	if !d.Contains("scary.example.com") {
		t.Fatal("expected scary.example.com to still be present")
	}
}

func TestDenylist_Remove_NonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "denylist.txt")
	d, _ := Load(path)
	if err := d.Remove("never.there.example"); err != nil {
		t.Fatalf("Remove of missing host should be idempotent, got: %v", err)
	}
}

func TestDenylist_Remove_PreservesFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip()
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "denylist.txt")
	d, _ := Load(path)
	_ = d.Add("a.example.com")
	_ = d.Add("b.example.com")
	_ = d.Remove("a.example.com")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected 0600, got %o", mode)
	}
}
