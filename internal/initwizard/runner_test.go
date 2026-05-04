package initwizard

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	callOrder          []string
	ports              []int
	summaryCounts      []int
	suggestedHosts     []HostSuggestion
}

func (m *mockPrompter) PromptWelcome(port int) error {
	m.callOrder = append(m.callOrder, "welcome")
	m.ports = append(m.ports, port)
	return nil
}
func (m *mockPrompter) PromptHosts(suggested []HostSuggestion, port int) ([]string, error) {
	m.callOrder = append(m.callOrder, "hosts")
	m.ports = append(m.ports, port)
	m.suggestedHosts = append([]HostSuggestion(nil), suggested...)
	if m.selectedHosts != nil {
		return m.selectedHosts, nil
	}
	return suggestionHosts(suggested), nil
}
func (m *mockPrompter) PromptCustomHosts(port int) ([]string, error) {
	m.callOrder = append(m.callOrder, "custom")
	m.ports = append(m.ports, port)
	return m.addCustom, nil
}
func (m *mockPrompter) PromptThreeListNote(port int) error {
	m.callOrder = append(m.callOrder, "threelist")
	m.ports = append(m.ports, port)
	return nil
}
func (m *mockPrompter) PromptPolicySummary(quietCount int, port int) error {
	m.callOrder = append(m.callOrder, "summary")
	m.ports = append(m.ports, port)
	m.summaryCounts = append(m.summaryCounts, quietCount)
	return nil
}
func (m *mockPrompter) PromptInstallCert() (bool, error) {
	m.callOrder = append(m.callOrder, "installcert")
	return m.confirmInstallCert, nil
}
func (m *mockPrompter) PromptSmokeTest() (bool, error) {
	m.callOrder = append(m.callOrder, "smoketest")
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

func TestRunner_PromptSequenceMatchesDesign(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	_ = os.WriteFile(cfg, []byte(`
[ports]
dashboard = 9000
`), 0o600)
	prompter := &mockPrompter{}
	opts := Options{
		ConfigPath:  cfg,
		ConfigDir:   dir,
		DataDir:     filepath.Join(dir, "data"),
		Installer:   &ca.MockInstaller{},
		Prompter:    prompter,
		Force:       true,
		InstallCert: InstallCertAuto,
		Detector: func() []agentdetect.DetectedAgent {
			return []agentdetect.DetectedAgent{
				{Name: "claude", Source: agentdetect.SourcePath, SuggestedHosts: []string{"api.anthropic.com"}},
				{Name: "codex", Source: agentdetect.SourcePath, SuggestedHosts: []string{"api.openai.com"}},
			}
		},
	}
	if err := Run(opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantOrder := []string{"welcome", "threelist", "hosts", "custom", "summary", "installcert"}
	if !reflect.DeepEqual(prompter.callOrder, wantOrder) {
		t.Fatalf("call order: got %v, want %v", prompter.callOrder, wantOrder)
	}
	wantPorts := []int{9000, 9000, 9000, 9000, 9000}
	if !reflect.DeepEqual(prompter.ports, wantPorts) {
		t.Fatalf("prompt ports: got %v, want %v", prompter.ports, wantPorts)
	}
	if !reflect.DeepEqual(prompter.summaryCounts, []int{2}) {
		t.Fatalf("summary counts: got %v, want [2]", prompter.summaryCounts)
	}
}

func TestRunner_QuietSkipsWelcomeAndSummary(t *testing.T) {
	dir := t.TempDir()
	prompter := &mockPrompter{}
	opts := Options{
		ConfigPath:  filepath.Join(dir, "config.toml"),
		ConfigDir:   dir,
		DataDir:     filepath.Join(dir, "data"),
		Installer:   &ca.MockInstaller{},
		Prompter:    prompter,
		Quiet:       true,
		InstallCert: InstallCertFalse,
		Detector:    func() []agentdetect.DetectedAgent { return nil },
	}
	if err := Run(opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	wantOrder := []string{"threelist", "hosts", "custom"}
	if !reflect.DeepEqual(prompter.callOrder, wantOrder) {
		t.Fatalf("call order: got %v, want %v", prompter.callOrder, wantOrder)
	}
	if len(prompter.summaryCounts) != 0 {
		t.Fatalf("quiet mode should skip summary, got counts %v", prompter.summaryCounts)
	}
}

func TestRunner_NonInteractive_SkipsNewPrompts(t *testing.T) {
	dir := t.TempDir()
	unusedPrompter := &mockPrompter{}
	opts := Options{
		ConfigPath:    filepath.Join(dir, "config.toml"),
		ConfigDir:     dir,
		DataDir:       filepath.Join(dir, "data"),
		Installer:     &ca.MockInstaller{},
		Prompter:      nil,
		AllowHosts:    []string{"api.anthropic.com"},
		InstallCert:   InstallCertFalse,
		SkipSmokeTest: true,
		Detector:      func() []agentdetect.DetectedAgent { return nil },
	}
	if err := Run(opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(unusedPrompter.callOrder) != 0 {
		t.Fatalf("non-interactive run invoked prompts: %v", unusedPrompter.callOrder)
	}
}

func TestRunner_PolicySummary_EmptyHostsCount(t *testing.T) {
	tests := []struct {
		name          string
		allowHosts    []string
		selectedHosts []string
		wantCount     int
	}{
		{
			name:      "fallback suggestion accepted",
			wantCount: 1,
		},
		{
			name:          "fallback suggestion cleared by user",
			selectedHosts: []string{},
			wantCount:     0,
		},
		{
			name:       "invalid allow hosts bypass fallback",
			allowHosts: []string{"not a host"},
			wantCount:  0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			prompter := &mockPrompter{selectedHosts: tt.selectedHosts}
			opts := Options{
				ConfigPath:  filepath.Join(dir, "config.toml"),
				ConfigDir:   dir,
				DataDir:     filepath.Join(dir, "data"),
				Installer:   &ca.MockInstaller{},
				Prompter:    prompter,
				AllowHosts:  tt.allowHosts,
				InstallCert: InstallCertFalse,
				Detector:    func() []agentdetect.DetectedAgent { return nil },
			}
			if err := Run(opts); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if !reflect.DeepEqual(prompter.summaryCounts, []int{tt.wantCount}) {
				t.Fatalf("summary counts: got %v, want [%d]", prompter.summaryCounts, tt.wantCount)
			}
		})
	}
}

func TestSuggestHosts_LabelsDetectedAndCustomHosts(t *testing.T) {
	detected := suggestHosts(nil, []agentdetect.DetectedAgent{
		{Name: "claude", SuggestedHosts: []string{"api.anthropic.com", "api.shared.example.com"}},
		{Name: "codex", SuggestedHosts: []string{"api.openai.com", "api.shared.example.com"}},
	})
	wantDetected := []HostSuggestion{
		{Host: "api.anthropic.com", Agents: []string{"claude code"}},
		{Host: "api.openai.com", Agents: []string{"codex"}},
		{Host: "api.shared.example.com", Agents: []string{"claude code", "codex"}},
	}
	if !reflect.DeepEqual(detected, wantDetected) {
		t.Fatalf("detected suggestions:\ngot  %#v\nwant %#v", detected, wantDetected)
	}

	custom := suggestHosts([]string{"override.example.com"}, nil)
	wantCustom := []HostSuggestion{{Host: "override.example.com", Agents: []string{"custom"}}}
	if !reflect.DeepEqual(custom, wantCustom) {
		t.Fatalf("custom suggestions: got %#v, want %#v", custom, wantCustom)
	}
}

// Custom hosts must always be offered after the multi-select, even when
// detection or --allow-host already produced a non-empty suggestion list.
// Previously the custom-host prompt was gated behind "no agents detected
// AND no --allow-host", which silently dropped user-typed hosts whenever
// the detector found anything.
func TestRunner_InteractivePromptAppendsCustomHostsEvenWhenAgentsDetected(t *testing.T) {
	dir := t.TempDir()
	prompter := &mockPrompter{
		selectedHosts: []string{"api.anthropic.com"},
		addCustom:     []string{"api.deepseek.com"},
	}
	opts := Options{
		ConfigPath:    filepath.Join(dir, "config.toml"),
		ConfigDir:     dir,
		DataDir:       filepath.Join(dir, "data"),
		Installer:     &ca.MockInstaller{},
		Prompter:      prompter,
		InstallCert:   InstallCertFalse,
		SkipSmokeTest: true,
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
	got := string(al)
	if !strings.Contains(got, "api.anthropic.com") {
		t.Errorf("allowlist missing detected host: %q", got)
	}
	if !strings.Contains(got, "api.deepseek.com") {
		t.Errorf("allowlist missing custom-typed host: %q", got)
	}
}

func TestRunner_InteractivePromptAppendsCustomHostsEvenWhenAllowHostFlagPassed(t *testing.T) {
	dir := t.TempDir()
	prompter := &mockPrompter{
		selectedHosts: []string{"override.example.com"},
		addCustom:     []string{"another.example.com"},
	}
	opts := Options{
		ConfigPath:    filepath.Join(dir, "config.toml"),
		ConfigDir:     dir,
		DataDir:       filepath.Join(dir, "data"),
		Installer:     &ca.MockInstaller{},
		Prompter:      prompter,
		AllowHosts:    []string{"override.example.com"},
		InstallCert:   InstallCertFalse,
		SkipSmokeTest: true,
		Detector:      func() []agentdetect.DetectedAgent { return nil },
	}
	if err := Run(opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	al, _ := os.ReadFile(filepath.Join(dir, "allowlist.txt"))
	got := string(al)
	if !strings.Contains(got, "override.example.com") || !strings.Contains(got, "another.example.com") {
		t.Errorf("custom host not appended when --allow-host present; got: %q", got)
	}
}
