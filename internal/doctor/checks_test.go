package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckCAFiles_HappyPath(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	_ = os.WriteFile(cert, []byte("CERT"), 0o644)
	_ = os.WriteFile(key, []byte("KEY"), 0o600)
	r := CheckCAFiles(dir)
	if r.Status != StatusOK {
		t.Fatalf("expected OK, got %v: %s", r.Status, r.Detail)
	}
}

func TestCheckCAFiles_KeyTooWide(t *testing.T) {
	if isWindows() {
		t.Skip("Windows ignores unix file modes")
	}
	dir := t.TempDir()
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	_ = os.WriteFile(cert, []byte("CERT"), 0o644)
	_ = os.WriteFile(key, []byte("KEY"), 0o644)
	r := CheckCAFiles(dir)
	if r.Status != StatusFail {
		t.Fatalf("expected FAIL on 0644 key.pem, got %v: %s", r.Status, r.Detail)
	}
}

func TestCheckCAFiles_Missing(t *testing.T) {
	dir := t.TempDir()
	r := CheckCAFiles(dir)
	if r.Status != StatusFail {
		t.Fatalf("expected FAIL on missing files, got %v: %s", r.Status, r.Detail)
	}
}

func TestCheckPortBindable(t *testing.T) {
	r := CheckPortBindable(0, "test")
	if r.Status != StatusOK {
		t.Fatalf("expected OK on port 0, got %v: %s", r.Status, r.Detail)
	}
}

func TestCheckDataDir_OK(t *testing.T) {
	dir := t.TempDir()
	if !isWindows() {
		_ = os.Chmod(dir, 0o700)
	}
	r := CheckDataDir(dir)
	if r.Status != StatusOK {
		t.Fatalf("expected OK on tempdir, got %v: %s", r.Status, r.Detail)
	}
}

func TestCheckDataDir_Missing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	r := CheckDataDir(dir)
	if r.Status != StatusFail {
		t.Fatalf("expected FAIL on missing dir, got %v: %s", r.Status, r.Detail)
	}
}

func TestCheckLockfile_None(t *testing.T) {
	dir := t.TempDir()
	r := CheckLockfile(filepath.Join(dir, "no-such-lock"))
	if r.Status != StatusOK {
		t.Fatalf("expected OK when lockfile absent, got %v: %s", r.Status, r.Detail)
	}
}

func TestCheckHostListFile_AbsentIsSkip(t *testing.T) {
	dir := t.TempDir()
	r := CheckHostListFile(filepath.Join(dir, "missing.txt"), "missing")
	if r.Status != StatusSkip {
		t.Fatalf("expected Skip when host list absent, got %v: %s", r.Status, r.Detail)
	}
}

func TestCheckHostListFile_BadMode(t *testing.T) {
	if isWindows() {
		t.Skip()
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "allowlist.txt")
	_ = os.WriteFile(path, []byte("api.example.com\n"), 0o644)
	r := CheckHostListFile(path, "allowlist")
	if r.Status != StatusWarn {
		t.Fatalf("expected Warn on 0644 list file, got %v", r.Status)
	}
}

func isWindows() bool { return os.PathSeparator == '\\' }
