package launcher

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func writeMinimalConfig(t *testing.T) (configPath, dataDir string) {
	t.Helper()
	tmp := t.TempDir()
	dataDir = filepath.Join(tmp, "data")
	configPath = filepath.Join(tmp, "config.toml")
	body := fmt.Sprintf("[ports]\nproxy = %d\ndashboard = %d\n[storage]\ndata_dir = '%s'\n",
		freePort(t), freePort(t), filepath.ToSlash(dataDir))
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

func TestBuildChildEnvScrubsLowercaseProxyVars(t *testing.T) {
	got := buildChildEnv([]string{
		"PATH=/bin",
		"https_proxy=http://wrong.example:8080",
		"http_proxy=http://wrong.example:8080",
		"all_proxy=socks5://wrong.example:1080",
		"no_proxy=*",
		"HTTPS_PROXY=http://wrong.example:8080",
		"HTTP_PROXY=http://wrong.example:8080",
		"ALL_PROXY=socks5://wrong.example:1080",
		"NO_PROXY=*",
	}, "127.0.0.1:18888")

	body := strings.Join(got, "\n")
	for _, forbidden := range []string{"wrong.example", "no_proxy=*", "https_proxy=", "http_proxy=", "all_proxy="} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("buildChildEnv leaked %q in:\n%s", forbidden, body)
		}
	}
	for _, want := range []string{
		"HTTPS_PROXY=http://127.0.0.1:18888",
		"HTTP_PROXY=http://127.0.0.1:18888",
		"ALL_PROXY=http://127.0.0.1:18888",
		"NO_PROXY=",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("buildChildEnv missing %q in:\n%s", want, body)
		}
	}
}

func TestProxyOptionsForListenerCarriesPassthroughHost(t *testing.T) {
	passthrough := func(host string) bool { return host == "mcp-proxy.anthropic.com" }
	opts := proxyOptionsForListener(proxyRunConfig{
		passthroughHost: passthrough,
	})
	if opts.PassthroughHost == nil {
		t.Fatal("expected PassthroughHost to be wired")
	}
	if !opts.PassthroughHost("mcp-proxy.anthropic.com") {
		t.Fatal("expected PassthroughHost predicate to be preserved")
	}
}

func TestProxyOptionsForListenerCarriesHijackHost(t *testing.T) {
	hijack := func(host string) bool { return host == "chatgpt.com" }
	opts := proxyOptionsForListener(proxyRunConfig{
		hijackHost: hijack,
	})
	if opts.HijackHost == nil {
		t.Fatal("expected HijackHost to be wired")
	}
	if !opts.HijackHost("chatgpt.com") {
		t.Fatal("expected HijackHost predicate to be preserved")
	}
}

func TestBuildHijackHostPredicateEmptyHostsReturnsNil(t *testing.T) {
	if buildHijackHostPredicate(nil, nil) != nil {
		t.Fatal("expected nil predicate when no hosts requested")
	}
	if buildHijackHostPredicate(nil, []string{"", "  "}) != nil {
		t.Fatal("expected nil predicate when only blank hosts requested")
	}
}

func TestBuildHijackHostPredicateNormalizesAndMatches(t *testing.T) {
	p := buildHijackHostPredicate(nil, []string{"ChatGPT.com", " other.example "})
	if p == nil {
		t.Fatal("expected non-nil predicate")
	}
	cases := map[string]bool{
		"chatgpt.com":     true,
		"CHATGPT.COM":     true,
		"other.example":   true,
		"api.openai.com":  false,
		"sub.chatgpt.com": false, // exact match only — wildcard support is intentionally out of scope
		"":                false,
	}
	for host, want := range cases {
		if got := p(host); got != want {
			t.Errorf("predicate(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestRunStatusLine_DashboardIsClickableURL(t *testing.T) {
	got := runStatusLine("airtight", "127.0.0.1:8888", "127.0.0.1:7878")
	if !strings.Contains(got, "dashboard http://127.0.0.1:7878") {
		t.Fatalf("status line should include clickable dashboard URL, got %q", got)
	}
	if strings.Contains(got, "dashboard 127.0.0.1:7878") {
		t.Fatalf("status line kept non-clickable dashboard address: %q", got)
	}
}

func TestExecArgvIncludesCommandAsArgv0(t *testing.T) {
	got := execArgv("/usr/local/bin/helper", []string{"-exit", "0"})
	want := []string{"/usr/local/bin/helper", "-exit", "0"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("execArgv() = %#v, want %#v", got, want)
	}
}

func TestSupervisor_LockfileEnforced(t *testing.T) {
	configPath, dataDir := writeMinimalConfig(t)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Write the *test's own* PID to the lockfile — it's guaranteed alive,
	// so the stale-lock-reclamation path won't kick in and the run must
	// fail with a held-lock error.
	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(filepath.Join(dataDir, "agent-gate.lock"), []byte(pid), 0o600); err != nil {
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

// TestSupervisor_StaleLockfileReclaimed verifies that a lockfile pointing
// at a non-running PID is silently reclaimed.
func TestSupervisor_StaleLockfileReclaimed(t *testing.T) {
	configPath, dataDir := writeMinimalConfig(t)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// PID 999999 is vanishingly unlikely to be alive on any test host.
	if err := os.WriteFile(filepath.Join(dataDir, "agent-gate.lock"), []byte("999999"), 0o600); err != nil {
		t.Fatal(err)
	}
	exit, err := Run(context.Background(), Options{
		Mode:       Permissive,
		ConfigPath: configPath,
		Cmd:        selfBinary(t),
		Args:       []string{"-exit", "0"},
		Stdout:     os.Stderr,
		Stderr:     os.Stderr,
	})
	if err != nil {
		t.Fatalf("expected stale lock to be reclaimed; got error: %v", err)
	}
	if exit != 0 {
		t.Fatalf("expected exit 0 after reclaiming stale lock; got %d", exit)
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
