package initwizard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agent-gate/internal/agentdetect"
	"agent-gate/internal/ca"
	"agent-gate/internal/config"

	"github.com/BurntSushi/toml"
)

var ErrConfigExists = errors.New("init: config already exists; pass --force to overwrite")

// Prompter is the interactive surface. nil Prompter on Options = non-interactive.
type Prompter interface {
	PromptHosts(suggested []string) ([]string, error)
	PromptCustomHosts() ([]string, error)
	PromptThreeListNote() error
	PromptInstallCert() (bool, error)
	PromptSmokeTest() (bool, error)
}

type InstallCertMode int

const (
	InstallCertAuto InstallCertMode = iota
	InstallCertTrue
	InstallCertFalse
)

type Options struct {
	ConfigPath    string
	ConfigDir     string
	DataDir       string
	Installer     ca.Installer
	Prompter      Prompter // nil → non-interactive
	Force         bool
	AllowHosts    []string
	InstallCert   InstallCertMode
	SkipSmokeTest bool
	DryRun        bool
	PrintConfig   bool
	Detector      func() []agentdetect.DetectedAgent
}

// Run executes the init flow. Pure orchestration; no lockfile or sudo.
// (init_cmd.go in cmd/agent-gate handles lockfile + interactive vs not.)
func Run(opts Options) error {
	if _, err := os.Stat(opts.ConfigPath); err == nil && !opts.Force && !opts.PrintConfig && !opts.DryRun {
		return ErrConfigExists
	}

	if !opts.DryRun && !opts.PrintConfig {
		if err := os.MkdirAll(opts.ConfigDir, 0o700); err != nil {
			return fmt.Errorf("config dir: %w", err)
		}
		if err := os.MkdirAll(opts.DataDir, 0o700); err != nil {
			return fmt.Errorf("data dir: %w", err)
		}
	}

	var agents []agentdetect.DetectedAgent
	if opts.Detector != nil {
		agents = opts.Detector()
	}

	hosts := suggestHosts(opts.AllowHosts, agents)

	finalHosts := hosts
	if opts.Prompter != nil {
		selected, err := opts.Prompter.PromptHosts(hosts)
		if err != nil {
			return fmt.Errorf("prompt hosts: %w", err)
		}
		finalHosts = selected
		extras, perr := opts.Prompter.PromptCustomHosts()
		if perr == nil {
			finalHosts = append(finalHosts, extras...)
		}
		_ = opts.Prompter.PromptThreeListNote()
	}
	finalHosts = dedupSort(finalHosts)

	tomlBytes, err := renderConfig(opts.ConfigPath)
	if err != nil {
		return err
	}

	if opts.PrintConfig {
		os.Stdout.Write(tomlBytes)
		return nil
	}
	if opts.DryRun {
		fmt.Fprintln(os.Stderr, "--dry-run: would write config.toml:")
		os.Stderr.Write(tomlBytes)
		fmt.Fprintf(os.Stderr, "\n--dry-run: would seed allowlist with: %v\n", finalHosts)
		return nil
	}

	if err := os.WriteFile(opts.ConfigPath, tomlBytes, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := appendAllowlist(filepath.Join(opts.ConfigDir, "allowlist.txt"), finalHosts); err != nil {
		return fmt.Errorf("seed allowlist: %w", err)
	}

	doInstall := opts.InstallCert == InstallCertTrue ||
		(opts.InstallCert == InstallCertAuto && opts.Prompter != nil)
	if doInstall && opts.Prompter != nil {
		ok, perr := opts.Prompter.PromptInstallCert()
		if perr == nil && !ok {
			doInstall = false
		}
	}
	if doInstall && opts.Installer != nil {
		caDir := filepath.Join(opts.ConfigDir, "ca")
		certPath := filepath.Join(caDir, "cert.pem")
		if err := opts.Installer.InstallFile(certPath); err != nil {
			return fmt.Errorf("cert install: %w", err)
		}
	}

	// Smoke-test prompt is deferred until the in-process proxy + curl probe
	// is implemented. Asking the user a question we can't answer would be a
	// UX wart, so the prompt is gated off entirely. PromptSmokeTest stays on
	// the Prompter interface for the eventual wiring.
	_ = opts.SkipSmokeTest

	fmt.Fprintln(os.Stderr, "agent-gate init: complete")
	return nil
}

func suggestHosts(allowFlag []string, agents []agentdetect.DetectedAgent) []string {
	if len(allowFlag) > 0 {
		return dedupSort(allowFlag) // --allow-host REPLACES detection
	}
	var hosts []string
	for _, a := range agents {
		hosts = append(hosts, a.SuggestedHosts...)
	}
	if len(hosts) == 0 {
		hosts = []string{"api.anthropic.com"}
	}
	return dedupSort(hosts)
}

func dedupSort(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if !isPlainHostname(s) {
			// Silently drop non-plain values rather than corrupt allowlist.txt.
			// agentdetect already normalizes via url.Parse + idna; CLI flag values
			// are the only path that can carry a scheme/port/slash by accident.
			if s != "" {
				fmt.Fprintf(os.Stderr, "init: ignoring %q (not a plain hostname)\n", s)
			}
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// isPlainHostname mirrors internal/allowlist's plain-host check.
func isPlainHostname(s string) bool {
	if s == "" {
		return false
	}
	if strings.Contains(s, "://") || strings.Contains(s, "/") || strings.ContainsRune(s, ':') {
		return false
	}
	if strings.ContainsAny(s, " \t#") {
		return false
	}
	return true
}

func appendAllowlist(path string, hosts []string) error {
	if len(hosts) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	existing := map[string]struct{}{}
	if data, err := os.ReadFile(path); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if i := strings.IndexByte(line, '#'); i >= 0 {
				line = line[:i]
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			existing[strings.ToLower(line)] = struct{}{}
		}
	}
	for _, h := range hosts {
		existing[strings.ToLower(h)] = struct{}{}
	}
	out := make([]string, 0, len(existing))
	for h := range existing {
		out = append(out, h)
	}
	sort.Strings(out)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(out, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func renderConfig(existingPath string) ([]byte, error) {
	const tmpl = `# agent-gate config — single-user audit gate for AI agents.
# All settings are local; nothing is ever sent off this machine.

[capture]
# "airtight" forces all subprocess egress through the proxy via a per-OS
# network jail. "permissive" only sets HTTPS_PROXY env vars (testing only).
default_mode = "%s"

[ports]
# Loopback-only. Both refuse non-127.0.0.1 binds.
proxy = %d       # TLS-MITM proxy
dashboard = %d   # Local web UI for review

[storage]
# Where captured events are persisted (JSONL + SQLite index).
data_dir = "%s"
# How often to rotate JSONL: "daily" | "weekly" | "never"
rotate = "%s"
# Compress rotated files older than this duration.
gzip_after = "%s"

[allowlist]
# When true, the proxy returns 403 for any request whose host isn't in
# allowlist.txt. When false (default), unknown hosts pass but flag the event.
enforce = %v

[rules]
# Disable specific built-in rules by ID. Use ` + "`agent-gate doctor`" + ` to list IDs.
disable = []
`
	cfg := config.Defaults()
	if data, err := os.ReadFile(existingPath); err == nil {
		var existing config.Config
		if _, derr := toml.Decode(string(data), &existing); derr == nil {
			if existing.Capture.DefaultMode != "" {
				cfg.Capture.DefaultMode = existing.Capture.DefaultMode
			}
			if existing.Ports.Proxy != 0 {
				cfg.Ports.Proxy = existing.Ports.Proxy
			}
			if existing.Ports.Dashboard != 0 {
				cfg.Ports.Dashboard = existing.Ports.Dashboard
			}
			if existing.Storage.DataDir != "" {
				cfg.Storage.DataDir = existing.Storage.DataDir
			}
			if existing.Storage.Rotate != "" {
				cfg.Storage.Rotate = existing.Storage.Rotate
			}
			if existing.Storage.GzipAfter != "" {
				cfg.Storage.GzipAfter = existing.Storage.GzipAfter
			}
			cfg.Allowlist.Enforce = existing.Allowlist.Enforce
		}
	}
	out := fmt.Sprintf(tmpl,
		cfg.Capture.DefaultMode, cfg.Ports.Proxy, cfg.Ports.Dashboard,
		cfg.Storage.DataDir, cfg.Storage.Rotate, cfg.Storage.GzipAfter,
		cfg.Allowlist.Enforce)
	return []byte(out), nil
}
