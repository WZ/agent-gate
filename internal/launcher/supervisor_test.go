package launcher

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeMinimalConfig(t *testing.T) (configPath, dataDir string) {
	t.Helper()
	tmp := t.TempDir()
	dataDir = filepath.Join(tmp, "data")
	configPath = filepath.Join(tmp, "config.toml")
	body := fmt.Sprintf("[ports]\nproxy = %d\ndashboard = %d\n[storage]\ndata_dir = %q\n",
		freePort(t), freePort(t), dataDir)
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func selfBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "testhelper")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bin, "agent-gate/internal/launcher/testhelper").CombinedOutput()
	if err != nil {
		t.Fatalf("build testhelper: %v\n%s", err, out)
	}
	return bin
}

func TestSupervisor_ChildExitCodePropagation(t *testing.T) {
	configPath, _ := writeMinimalConfig(t)
	exit, err := Run(context.Background(), Options{
		Mode:       Permissive,
		ConfigPath: configPath,
		Cmd:        selfBinary(t),
		Args:       []string{"-exit", "42"},
		Stdout:     os.Stderr,
		Stderr:     os.Stderr,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if exit != 42 {
		t.Fatalf("want exit=42, got %d", exit)
	}
}

func TestSupervisor_LockfileEnforced(t *testing.T) {
	configPath, dataDir := writeMinimalConfig(t)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "agent-gate.lock"), []byte("99999"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Run(context.Background(), Options{
		Mode:       Permissive,
		ConfigPath: configPath,
		Cmd:        selfBinary(t),
		Args:       []string{"-exit", "0"},
	})
	if err == nil || !strings.Contains(err.Error(), "another agent-gate run is active") {
		t.Fatalf("expected lockfile error, got %v", err)
	}
}

// TestSupervisor_PermissiveSetsProxyEnv: spawn a target that prints its env;
// confirm HTTPS_PROXY points at our proxy. Skipped on Windows because the
// helper command shape (`/bin/sh -c`) is unix-only.
func TestSupervisor_PermissiveSetsProxyEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("env-printing helper is unix-shell-shaped")
	}
	configPath, _ := writeMinimalConfig(t)
	tmp := t.TempDir()
	out := filepath.Join(tmp, "env.txt")
	exit, err := Run(context.Background(), Options{
		Mode:       Permissive,
		ConfigPath: configPath,
		Cmd:        "/bin/sh",
		Args:       []string{"-c", "env | grep -E '^(HTTPS_PROXY|HTTP_PROXY|ALL_PROXY|NO_PROXY)=' > " + out},
	})
	if err != nil || exit != 0 {
		t.Fatalf("Run: exit=%d err=%v", exit, err)
	}
	body, _ := os.ReadFile(out)
	if !strings.Contains(string(body), "HTTPS_PROXY=http://127.0.0.1:") {
		t.Fatalf("expected HTTPS_PROXY in env; got:\n%s", body)
	}
}
