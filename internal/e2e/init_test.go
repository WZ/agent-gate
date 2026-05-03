package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInit_NonInteractive_HappyPath builds the agent-gate binary, runs
// `init --non-interactive --install-cert=false` against a tempdir-rooted
// config, and asserts the artifacts are correct: config.toml, ca/cert.pem,
// ca/key.pem, allowlist.txt with the seeded hosts.
func TestInit_NonInteractive_HappyPath(t *testing.T) {
	bin := buildAgentGateBin(t)
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")

	cmd := exec.Command(bin,
		"init",
		"--config", cfg,
		"--non-interactive",
		"--install-cert=false",
		"--allow-host", "api.anthropic.com",
		"--allow-host", "api.openai.com",
	)
	cmd.Env = append(os.Environ(),
		"HOME="+tmp,
		"XDG_CONFIG_HOME="+tmp+"/cfg",
		"XDG_DATA_HOME="+tmp+"/data",
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("init: %v\nstderr: %s", err, stderr.String())
	}
	for _, want := range []string{
		cfg,
		filepath.Join(tmp, "ca", "cert.pem"),
		filepath.Join(tmp, "ca", "key.pem"),
		filepath.Join(tmp, "allowlist.txt"),
	} {
		if _, err := os.Stat(want); err != nil {
			t.Fatalf("missing artifact %s: %v", want, err)
		}
	}
	allow, _ := os.ReadFile(filepath.Join(tmp, "allowlist.txt"))
	for _, h := range []string{"api.anthropic.com", "api.openai.com"} {
		if !strings.Contains(string(allow), h) {
			t.Errorf("allowlist missing %q: %s", h, string(allow))
		}
	}

	// Re-running without --force should fail.
	cmd = exec.Command(bin, "init", "--config", cfg, "--non-interactive", "--install-cert=false")
	cmd.Env = append(os.Environ(),
		"HOME="+tmp,
		"XDG_CONFIG_HOME="+tmp+"/cfg",
		"XDG_DATA_HOME="+tmp+"/data",
	)
	var stderr2 bytes.Buffer
	cmd.Stderr = &stderr2
	if err := cmd.Run(); err == nil {
		t.Fatal("expected error on re-init without --force")
	}
	if !strings.Contains(stderr2.String(), "already exists") && !strings.Contains(stderr2.String(), "--force") {
		t.Errorf("error message should mention --force; got: %s", stderr2.String())
	}

	// --force preserves user-set values: edit the config to non-default port,
	// re-run with --force --allow-host, verify port preserved AND new host added.
	contents, _ := os.ReadFile(cfg)
	edited := strings.Replace(string(contents), "proxy = 8888", "proxy = 18888", 1)
	if err := os.WriteFile(cfg, []byte(edited), 0o600); err != nil {
		t.Fatalf("rewrite cfg: %v", err)
	}

	cmd = exec.Command(bin, "init", "--config", cfg,
		"--non-interactive", "--install-cert=false", "--force",
		"--allow-host", "extra.example.com",
	)
	cmd.Env = append(os.Environ(),
		"HOME="+tmp,
		"XDG_CONFIG_HOME="+tmp+"/cfg",
		"XDG_DATA_HOME="+tmp+"/data",
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("init --force: %v", err)
	}
	contents, _ = os.ReadFile(cfg)
	if !strings.Contains(string(contents), "18888") {
		t.Errorf("--force lost user-set proxy=18888; got:\n%s", string(contents))
	}
	allow, _ = os.ReadFile(filepath.Join(tmp, "allowlist.txt"))
	for _, h := range []string{"api.anthropic.com", "api.openai.com", "extra.example.com"} {
		if !strings.Contains(string(allow), h) {
			t.Errorf("allowlist after --force missing %q; got:\n%s", h, string(allow))
		}
	}
}

func TestInit_PrintConfig_NoSideEffects(t *testing.T) {
	bin := buildAgentGateBin(t)
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")

	cmd := exec.Command(bin, "init", "--config", cfg, "--print-config", "--non-interactive")
	cmd.Env = append(os.Environ(),
		"HOME="+tmp,
		"XDG_CONFIG_HOME="+tmp+"/cfg",
		"XDG_DATA_HOME="+tmp+"/data",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("--print-config: %v", err)
	}
	if !strings.Contains(out.String(), "[ports]") {
		t.Errorf("--print-config did not emit TOML; got: %s", out.String())
	}
	if _, err := os.Stat(cfg); err == nil {
		t.Error("--print-config should not write config.toml")
	}
}

func TestDoctor_OnFreshInit(t *testing.T) {
	bin := buildAgentGateBin(t)
	tmp := t.TempDir()
	cfg := filepath.Join(tmp, "config.toml")

	cmd := exec.Command(bin, "init", "--config", cfg, "--non-interactive",
		"--install-cert=false", "--allow-host", "api.anthropic.com")
	cmd.Env = append(os.Environ(),
		"HOME="+tmp,
		"XDG_CONFIG_HOME="+tmp+"/cfg",
		"XDG_DATA_HOME="+tmp+"/data",
	)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("init: %v", err)
	}

	cmd = exec.Command(bin, "doctor", "--config", cfg)
	cmd.Env = append(os.Environ(),
		"HOME="+tmp,
		"XDG_CONFIG_HOME="+tmp+"/cfg",
		"XDG_DATA_HOME="+tmp+"/data",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run() // doctor may exit 1 (CA not in trust store under e2e); the report MUST print regardless.
	if !strings.Contains(out.String(), "agent-gate doctor") {
		t.Fatalf("doctor did not print report; got: %s", out.String())
	}
}
