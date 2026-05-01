package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestAirtight_DirectDialIsDenied is the load-bearing assertion for Plan 3:
// in airtight mode, a target that bypasses HTTPS_PROXY MUST fail to reach
// the network. This is what differentiates airtight from permissive.
//
// Skipped where airtight is unavailable: Windows (Plan 4), Linux without
// unprivileged user namespaces, or when `init` errors (e.g., needs admin).
func TestAirtight_DirectDialIsDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows airtight is pending Plan 4")
	}
	if testing.Short() {
		t.Skip("airtight e2e is slow under -short")
	}

	bin := buildAgentGateBin(t)
	helper := buildTestHelperBin(t)

	tmp := t.TempDir()
	configPath := writeAirtightConfig(t, tmp)

	if out, err := exec.Command(bin, "init", "--config", configPath).CombinedOutput(); err != nil {
		t.Skipf("init failed (likely needs admin/sysctl): %v\n%s", err, out)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "run", "--config", configPath, "--",
		helper, "-dial-direct", "1.1.1.1:80", "-timeout", "1s")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("airtight broken: dial-direct succeeded; expected kernel-level deny")
	}
	t.Logf("airtight correctly denied: %v", err)
}

func buildAgentGateBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "agent-gate")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	out, err := exec.Command("go", "build", "-o", bin, "agent-gate/cmd/agent-gate").CombinedOutput()
	if err != nil {
		t.Fatalf("build agent-gate: %v\n%s", err, out)
	}
	return bin
}

func buildTestHelperBin(t *testing.T) string {
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

func writeAirtightConfig(t *testing.T, root string) string {
	t.Helper()
	configDir := filepath.Join(root, "config")
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	body := fmt.Sprintf("[ports]\nproxy = %d\ndashboard = %d\n[storage]\ndata_dir = '%s'\n",
		pickFreePortE2E(t), pickFreePortE2E(t), filepath.ToSlash(dataDir))
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath
}

func pickFreePortE2E(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
