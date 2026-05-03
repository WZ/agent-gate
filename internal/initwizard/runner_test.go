package initwizard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-gate/internal/agentdetect"
	"agent-gate/internal/ca"
)

type mockPrompter struct {
	selectedHosts      []string
	addCustom          []string
	confirmInstallCert bool
	confirmSmokeTest   bool
}

func (m *mockPrompter) PromptHosts(suggested []string) ([]string, error) {
	if m.selectedHosts != nil {
		return m.selectedHosts, nil
	}
	return suggested, nil
}
func (m *mockPrompter) PromptCustomHosts() ([]string, error) {
	return m.addCustom, nil
}
func (m *mockPrompter) PromptThreeListNote() error {
	return nil
}
func (m *mockPrompter) PromptInstallCert() (bool, error) {
	return m.confirmInstallCert, nil
}
func (m *mockPrompter) PromptSmokeTest() (bool, error) {
	return m.confirmSmokeTest, nil
}

func TestRunner_NonInteractive_NoCert_HappyPath(t *testing.T) {
	dir := t.TempDir()
	mock := &ca.MockInstaller{}
	opts := Options{
		ConfigPath:    filepath.Join(dir, "config.toml"),
		ConfigDir:     dir,
		DataDir:       filepath.Join(dir, "data"),
		Installer:     mock,
		Prompter:      nil, // non-interactive
		Force:         false,
		AllowHosts:    []string{"api.anthropic.com"},
		InstallCert:   InstallCertFalse,
		SkipSmokeTest: true,
		Detector:      func() []agentdetect.DetectedAgent { return nil },
	}
	if err := Run(opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(opts.ConfigPath); err != nil {
		t.Fatalf("config.toml not written: %v", err)
	}
	al, err := os.ReadFile(filepath.Join(dir, "allowlist.txt"))
	if err != nil {
		t.Fatalf("allowlist.txt: %v", err)
	}
	if !strings.Contains(string(al), "api.anthropic.com") {
		t.Fatalf("allowlist not seeded: %q", string(al))
	}
	if len(mock.InstallCalls) != 0 {
		t.Fatalf("non-interactive --install-cert=false must not install, got %d calls", len(mock.InstallCalls))
	}
}

func TestRunner_RefusesExistingConfigWithoutForce(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	_ = os.WriteFile(cfg, []byte("existing"), 0o600)
	opts := Options{
		ConfigPath:  cfg,
		ConfigDir:   dir,
		DataDir:     filepath.Join(dir, "data"),
		Installer:   &ca.MockInstaller{},
		AllowHosts:  []string{"x.example.com"},
		InstallCert: InstallCertFalse,
		Detector:    func() []agentdetect.DetectedAgent { return nil },
	}
	err := Run(opts)
	if err == nil {
		t.Fatal("expected error on existing config without --force")
	}
	if !errors.Is(err, ErrConfigExists) {
		t.Fatalf("expected ErrConfigExists, got %v", err)
	}
	got, _ := os.ReadFile(cfg)
	if string(got) != "existing" {
		t.Fatalf("config.toml was overwritten without --force: %q", string(got))
	}
}

func TestRunner_ForceOverwritesAndPreservesUserPorts(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	_ = os.WriteFile(cfg, []byte(`
[capture]
default_mode = "permissive"
[ports]
proxy = 9999
dashboard = 9998
[storage]
data_dir = "/custom/data"
`), 0o600)
	opts := Options{
		ConfigPath:  cfg,
		ConfigDir:   dir,
		DataDir:     filepath.Join(dir, "data"),
		Installer:   &ca.MockInstaller{},
		AllowHosts:  []string{"x.example.com"},
		Force:       true,
		InstallCert: InstallCertFalse,
		Detector:    func() []agentdetect.DetectedAgent { return nil },
	}
	if err := Run(opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := os.ReadFile(cfg)
	if !strings.Contains(string(got), "9999") || !strings.Contains(string(got), "9998") {
		t.Fatalf("--force lost user-set ports; got:\n%s", string(got))
	}
	if !strings.Contains(string(got), "# agent-gate config") {
		t.Fatalf("--force did not write commented header; got:\n%s", string(got))
	}
}

func TestRunner_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	opts := Options{
		ConfigPath: cfg,
		ConfigDir:  dir,
		DataDir:    filepath.Join(dir, "data"),
		Installer:  &ca.MockInstaller{},
		AllowHosts: []string{"x.example.com"},
		DryRun:     true,
		Detector:   func() []agentdetect.DetectedAgent { return nil },
	}
	if err := Run(opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(cfg); err == nil {
		t.Errorf("--dry-run should not write config.toml")
	}
}

func TestRunner_AllowHostReplacesDetection(t *testing.T) {
	dir := t.TempDir()
	mock := &ca.MockInstaller{}
	opts := Options{
		ConfigPath:  filepath.Join(dir, "config.toml"),
		ConfigDir:   dir,
		DataDir:     filepath.Join(dir, "data"),
		Installer:   mock,
		AllowHosts:  []string{"override.example.com"},
		InstallCert: InstallCertFalse,
		Detector: func() []agentdetect.DetectedAgent {
			return []agentdetect.DetectedAgent{
				{Name: "claude", Source: agentdetect.SourcePath, SuggestedHosts: []string{"api.anthropic.com"}},
			}
		},
	}
	if err := Run(opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	al, _ := os.ReadFile(filepath.Join(dir, "allowlist.txt"))
	if !strings.Contains(string(al), "override.example.com") {
		t.Errorf("allowlist missing override host: %q", string(al))
	}
	if strings.Contains(string(al), "api.anthropic.com") {
		t.Errorf("--allow-host should REPLACE detection; got: %q", string(al))
	}
}

func TestHuhPrompter_SatisfiesInterface(t *testing.T) {
	var _ Prompter = HuhPrompter{}
}
