//go:build linux

package launcher

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func skipIfAirtightUnavailable(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	ok, reason := airtightFeasible()
	if !ok {
		t.Skipf("airtight unavailable: %s", reason)
	}
}

func buildLinuxTestHelper(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "testhelper")
	cmd := exec.Command("go", "build", "-o", bin, "agent-gate/internal/launcher/testhelper")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build testhelper: %v", err)
	}
	return bin
}

func startFakeProxyOnPort(t *testing.T, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("fake proxy listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
}

// TestSandboxLinux_RunIsolatesNonProxyEgress is the load-bearing test.
// It runs `launcher.Run` end-to-end with a target that tries to dial 1.1.1.1
// directly, and asserts that fails. The full pipeline is exercised — proxy,
// dashboard, helper, FD-passing.
func TestSandboxLinux_RunIsolatesNonProxyEgress(t *testing.T) {
	skipIfAirtightUnavailable(t)
	bin := buildLinuxTestHelper(t)

	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	port := pickFreePort(t)
	dashPort := pickFreePort(t)
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf(
		"[ports]\nproxy = %d\ndashboard = %d\n[storage]\ndata_dir = %q\n",
		port, dashPort, tmp+"/data")), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	exit, err := Run(ctx, Options{
		Mode:       Airtight,
		ConfigPath: configPath,
		Cmd:        bin,
		Args:       []string{"-dial-direct", "1.1.1.1:80", "-timeout", "1s"},
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The dial should fail in the netns → testhelper returns 1.
	if exit == 0 {
		t.Fatalf("expected dial-direct to fail under airtight; got exit 0")
	}
}

func TestSandboxLinux_DescendantInheritsNetns(t *testing.T) {
	skipIfAirtightUnavailable(t)
	bin := buildLinuxTestHelper(t)

	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	port := pickFreePort(t)
	dashPort := pickFreePort(t)
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf(
		"[ports]\nproxy = %d\ndashboard = %d\n[storage]\ndata_dir = %q\n",
		port, dashPort, tmp+"/data")), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	exit, err := Run(ctx, Options{
		Mode:       Airtight,
		ConfigPath: configPath,
		Cmd:        bin,
		Args: []string{"-spawn", bin, "--",
			"-dial-direct", "1.1.1.1:80", "-timeout", "1s"},
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit == 0 {
		t.Fatalf("expected descendant dial-direct to fail; got exit 0")
	}
}

func TestSandboxLinux_AirtightFailAbortsWhenUnsupported(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux only")
	}
	if ok, _ := airtightFeasible(); ok {
		t.Skip("airtight is feasible here; this test only runs when it isn't")
	}
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config.toml")
	port := pickFreePort(t)
	dashPort := pickFreePort(t)
	if err := os.WriteFile(configPath, []byte(fmt.Sprintf(
		"[ports]\nproxy = %d\ndashboard = %d\n[storage]\ndata_dir = %q\n",
		port, dashPort, tmp+"/data")), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := Run(ctx, Options{
		Mode:         Airtight,
		AirtightFail: true,
		ConfigPath:   configPath,
		Cmd:          "/bin/true",
	})
	if err == nil {
		t.Fatalf("expected --airtight-fail to error when unsupported")
	}
}

func pickFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
