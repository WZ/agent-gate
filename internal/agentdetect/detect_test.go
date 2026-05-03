package agentdetect

import (
	"errors"
	"os/exec"
	"testing"

	"golang.org/x/net/idna"
)

func mockPath(present map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if p, ok := present[name]; ok {
			return p, nil
		}
		return "", exec.ErrNotFound
	}
}

func mockEnv(values map[string]string) func(string) string {
	return func(k string) string { return values[k] }
}

func TestRun_NothingDetected(t *testing.T) {
	got := Run(Config{
		PathLookup: mockPath(nil),
		EnvGetter:  mockEnv(nil),
		IDNLookup:  idna.Lookup.ToASCII,
	})
	if len(got) != 0 {
		t.Fatalf("expected no agents, got %+v", got)
	}
}

func TestRun_DetectsClaudeOnPath(t *testing.T) {
	got := Run(Config{
		PathLookup: mockPath(map[string]string{"claude": "/usr/local/bin/claude"}),
		EnvGetter:  mockEnv(nil),
		IDNLookup:  idna.Lookup.ToASCII,
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(got))
	}
	a := got[0]
	if a.Name != "claude" || a.Source != SourcePath {
		t.Errorf("unexpected agent: %+v", a)
	}
	if !sameStrings(a.SuggestedHosts, []string{"api.anthropic.com"}) {
		t.Errorf("SuggestedHosts: got %v, want [api.anthropic.com]", a.SuggestedHosts)
	}
}

func TestRun_EnvOverridesDefault(t *testing.T) {
	got := Run(Config{
		PathLookup: mockPath(map[string]string{"claude": "/usr/local/bin/claude"}),
		EnvGetter:  mockEnv(map[string]string{"ANTHROPIC_BASE_URL": "https://anthropic.internal.example.com/v1"}),
		IDNLookup:  idna.Lookup.ToASCII,
	})
	if len(got) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(got))
	}
	a := got[0]
	if a.Source != SourceBoth {
		t.Errorf("Source: got %v, want SourceBoth", a.Source)
	}
	if a.EnvHost != "anthropic.internal.example.com" {
		t.Errorf("EnvHost: got %q", a.EnvHost)
	}
	if !sameStrings(a.SuggestedHosts, []string{"anthropic.internal.example.com"}) {
		t.Errorf("SuggestedHosts: got %v, want [anthropic.internal.example.com]", a.SuggestedHosts)
	}
}

func TestRun_RejectsIDNHomograph(t *testing.T) {
	cyrillicURL := "https://аpi.anthropic.com/v1" // first char is Cyrillic а (U+0430)
	got := Run(Config{
		PathLookup: mockPath(nil),
		EnvGetter:  mockEnv(map[string]string{"ANTHROPIC_BASE_URL": cyrillicURL}),
		IDNLookup:  idna.Lookup.ToASCII,
	})
	for _, a := range got {
		if containsCyrillic(a.EnvHost) {
			t.Fatalf("EnvHost contains Cyrillic chars after IDN normalization: %q", a.EnvHost)
		}
	}
}

func TestRun_DetectsCodexFromEnv(t *testing.T) {
	got := Run(Config{
		PathLookup: mockPath(nil),
		EnvGetter:  mockEnv(map[string]string{"OPENAI_BASE_URL": "https://api.openai.com/v1"}),
		IDNLookup:  idna.Lookup.ToASCII,
	})
	found := false
	for _, a := range got {
		if a.Name == "codex" && a.Source == SourceEnv {
			found = true
			if a.EnvHost != "api.openai.com" {
				t.Errorf("EnvHost: got %q", a.EnvHost)
			}
		}
	}
	if !found {
		t.Errorf("expected codex via env, got %+v", got)
	}
}

func TestRun_RejectsControlChars(t *testing.T) {
	got := Run(Config{
		PathLookup: mockPath(nil),
		EnvGetter:  mockEnv(map[string]string{"ANTHROPIC_BASE_URL": "https://api.anthropic.com\r\n.evil.example/v1"}),
		IDNLookup:  idna.Lookup.ToASCII,
	})
	for _, a := range got {
		if a.EnvHost != "" {
			t.Fatalf("expected empty EnvHost (rejected), got %q", a.EnvHost)
		}
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func containsCyrillic(s string) bool {
	for _, r := range s {
		if r >= 0x0400 && r <= 0x04FF {
			return true
		}
	}
	return false
}

var _ = errors.New
