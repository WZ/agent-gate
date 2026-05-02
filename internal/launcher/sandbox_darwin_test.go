//go:build darwin

package launcher

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// buildTestHelper compiles internal/launcher/testhelper into tmpdir and
// returns the absolute path of the resulting binary.
func buildTestHelper(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "testhelper")
	cmd := exec.Command("go", "build", "-o", bin, "agent-gate/internal/launcher/testhelper")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build testhelper: %v", err)
	}
	return bin
}

// startFakeProxy listens on 127.0.0.1:<port> and returns 200 to any GET.
func startFakeProxy(t *testing.T, port int) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", "18888"))
	if err != nil {
		t.Fatalf("fake proxy listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln
}

func TestSandboxDarwin_DeniesNonProxyTCP(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	bin := buildTestHelper(t)
	startFakeProxy(t, 18888)

	profile := buildSandboxProfile(18888)
	cmd := exec.Command("/usr/bin/sandbox-exec", "-p", profile, bin, "-dial-direct", "1.1.1.1:80", "-timeout", "1s")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected dial-direct to fail under sandbox; got success. Output: %s", out)
	}
	t.Logf("sandbox correctly denied: %s", out)
}

func TestSandboxDarwin_AllowsProxyTCP(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	bin := buildTestHelper(t)
	startFakeProxy(t, 18888)

	profile := buildSandboxProfile(18888)
	cmd := exec.Command("/usr/bin/sandbox-exec", "-p", profile, bin, "-dial", "127.0.0.1:18888", "-timeout", "2s")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("expected dial to proxy to succeed; got err=%v out=%s", err, out)
	}
}

func TestSandboxDarwin_DescendantInheritsSandbox(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	bin := buildTestHelper(t)
	startFakeProxy(t, 18888)

	profile := buildSandboxProfile(18888)
	cmd := exec.Command("/usr/bin/sandbox-exec", "-p", profile,
		bin, "-spawn", bin, "--", "-dial-direct", "1.1.1.1:80", "-timeout", "1s")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected descendant dial-direct to fail; got success. Output: %s", out)
	}
}

func TestSandboxDarwin_AirtightFeasible(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	ok, reason := airtightFeasible()
	if !ok {
		t.Fatalf("airtight should be feasible on darwin: %s", reason)
	}
}

// TestSandboxDarwin_RunIntegration exercises launcher.Run end to end with airtight on.
func TestSandboxDarwin_RunIntegration(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	if testing.Short() {
		t.Skip("skip in -short")
	}
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(configPath, []byte(
		"[ports]\nproxy = 18888\ndashboard = 17878\n[storage]\ndata_dir = '"+filepath.ToSlash(tmp)+"/data'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := buildTestHelper(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	exit, err := Run(ctx, Options{
		Mode:       Airtight,
		ConfigPath: configPath,
		Cmd:        bin,
		Args:       []string{"-exit", "0"},
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != 0 {
		t.Fatalf("want exit 0, got %d", exit)
	}
}
