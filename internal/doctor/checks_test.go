package doctor

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	r := CheckPortBindable(0, "test", "")
	if r.Status != StatusOK {
		t.Fatalf("expected OK on port 0, got %v: %s", r.Status, r.Detail)
	}
}

// When a port is bound by another process, CheckPortBindable returns OK
// if the lockfile names the user's own running agent-gate. Doctor must
// not tell the user "address already in use" while their own session is
// the legitimate holder.
func TestCheckPortBindable_LockfileExempts(t *testing.T) {
	// Bind a port to simulate "in use".
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	// Write our own PID to a lockfile — we are alive by definition.
	lockPath := filepath.Join(t.TempDir(), "agent-gate.lock")
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		t.Fatalf("write lockfile: %v", err)
	}

	r := CheckPortBindable(port, "proxy", lockPath)
	if r.Status != StatusOK {
		t.Fatalf("expected OK with live lockfile, got %v: %s", r.Status, r.Detail)
	}
	if !strings.Contains(r.Detail, "agent-gate run") {
		t.Errorf("expected detail to mention agent-gate run, got %q", r.Detail)
	}
}

// Without a lockfile the in-use check still fails — that's the correct
// "something else is squatting on the port" signal.
func TestCheckPortBindable_NoLockfileFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	r := CheckPortBindable(port, "proxy", "")
	if r.Status != StatusFail {
		t.Fatalf("expected FAIL with no lockfile, got %v: %s", r.Status, r.Detail)
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

func TestCheckConfigValid_Good(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	_ = os.WriteFile(path, []byte(`[capture]
default_mode = "airtight"
[ports]
proxy = 8888
dashboard = 7878
[storage]
data_dir = "/tmp/agent-gate"
[allowlist]
enforce = false
[rules]
disable = []
`), 0o600)
	r := CheckConfigValid(path)
	if r.Status != StatusOK {
		t.Fatalf("expected OK, got %v: %s", r.Status, r.Detail)
	}
}

func TestCheckConfigValid_Bad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	_ = os.WriteFile(path, []byte(`this is = not [valid toml`), 0o600)
	r := CheckConfigValid(path)
	if r.Status != StatusFail {
		t.Fatalf("expected FAIL on bad TOML, got %v: %s", r.Status, r.Detail)
	}
}

func TestCheckConfigValid_Missing(t *testing.T) {
	r := CheckConfigValid("/nonexistent/path/config.toml")
	if r.Status != StatusFail {
		t.Fatalf("expected FAIL on missing config, got %v", r.Status)
	}
}

func TestCheckAgentsDetected_Empty(t *testing.T) {
	r := CheckAgentsDetected(nil)
	if r.Status != StatusWarn {
		t.Fatalf("expected WARN on empty agents, got %v: %s", r.Status, r.Detail)
	}
}

func TestReport_WriteHuman_IncludesGlyphsAndSummary(t *testing.T) {
	var buf strings.Builder
	rep := Report{Results: []Result{
		{ID: "ca-files", Status: StatusOK, Detail: "ok"},
		{ID: "data-dir", Status: StatusFail, Detail: "missing", FixHint: "mkdir -p /x"},
	}}
	rep.WriteHuman(&buf)
	out := buf.String()
	for _, want := range []string{"✓", "✗", "1 failed", "Fix:", "mkdir -p /x"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- got ---\n%s", want, out)
		}
	}
}

func isWindows() bool { return os.PathSeparator == '\\' }
