package ca

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	tstore "github.com/smallstep/truststore"
)

// Installer manages CA certificate trust across system + Firefox stores.
// SmallstepInstaller wraps github.com/smallstep/truststore. MockInstaller
// (truststore_mock.go) is the test double; tests should never sudo.
type Installer interface {
	InstallFile(certPath string) error
	UninstallFile(certPath string) error
	ProbeAll(certPath string) []StoreProbe
}

// StoreProbe is the per-store install state. Used by `init`, `cert install`,
// and `doctor` to render one-line-per-store status.
//
// Skip means the store exists on disk but the user clearly doesn't use
// it — e.g. a Firefox profile directory with no cert9.db (the profile
// has never been opened by Firefox). Doctor renders Skip as "–" rather
// than treating it as a failure: the user shouldn't need to install the
// CA into a profile they don't use.
type StoreProbe struct {
	Store   string
	Present bool
	Skip    bool
	Note    string
	Err     error
}

// SmallstepInstaller is the production Installer. It wraps
// github.com/smallstep/truststore, which handles per-platform system trust
// store install (macOS Keychain via `security`, Linux `update-ca-certificates`
// or equivalent, Windows CurrentUser Root) plus Firefox NSS DBs in detected
// profiles when WithFirefox() is enabled.
type SmallstepInstaller struct{}

func (SmallstepInstaller) InstallFile(certPath string) error {
	if err := tstore.InstallFile(certPath, tstore.WithFirefox()); err != nil {
		return fmt.Errorf("truststore install: %w", err)
	}
	return nil
}

func (SmallstepInstaller) UninstallFile(certPath string) error {
	if err := tstore.UninstallFile(certPath, tstore.WithFirefox()); err != nil {
		return fmt.Errorf("truststore uninstall: %w", err)
	}
	return nil
}

func (SmallstepInstaller) ProbeAll(certPath string) []StoreProbe {
	out := []StoreProbe{}
	switch runtime.GOOS {
	case "darwin":
		out = append(out, probeMacOSKeychain(certPath))
	case "linux":
		out = append(out, probeLinuxCACertificates(certPath))
	case "windows":
		out = append(out, probeWindowsRoot(certPath))
	}
	for _, profile := range FirefoxProfiles() {
		out = append(out, probeFirefoxProfile(certPath, profile))
	}
	return out
}

// FirefoxProfiles returns absolute paths to all detected Firefox profile
// directories on the current platform. Empty if Firefox not installed.
func FirefoxProfiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var roots []string
	switch runtime.GOOS {
	case "darwin":
		roots = []string{filepath.Join(home, "Library/Application Support/Firefox/Profiles")}
	case "linux":
		roots = []string{
			filepath.Join(home, ".mozilla/firefox"),
			filepath.Join(home, "snap/firefox/common/.mozilla/firefox"),
		}
	case "windows":
		appData := os.Getenv("APPDATA")
		if appData == "" {
			return nil
		}
		roots = []string{filepath.Join(appData, "Mozilla", "Firefox", "Profiles")}
	default:
		return nil
	}
	var profiles []string
	for _, r := range roots {
		entries, err := os.ReadDir(r)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.Contains(name, ".default") || strings.Contains(name, ".dev-edition-default") {
				profiles = append(profiles, filepath.Join(r, name))
			}
		}
	}
	return profiles
}

// Per-platform probe stubs. v0.6.0 reports "trust truststore did its job"
// after a successful InstallFile; deeper inspection (security
// find-certificate / NSS DB query) can harden these in a follow-up.
func probeMacOSKeychain(certPath string) StoreProbe {
	return StoreProbe{
		Store:   "macos-keychain",
		Present: true,
		Note:    "(install-time confirmation)",
	}
}

func probeLinuxCACertificates(certPath string) StoreProbe {
	dest := "/etc/ssl/certs/agent-gate.pem"
	if _, err := os.Stat(dest); err == nil {
		return StoreProbe{Store: "linux-ca-certificates", Present: true, Note: dest}
	}
	return StoreProbe{Store: "linux-ca-certificates", Present: false, Note: dest + " not found"}
}

func probeWindowsRoot(certPath string) StoreProbe {
	return StoreProbe{
		Store:   "windows-root",
		Present: true,
		Note:    "(CurrentUser Root store; install-time confirmation)",
	}
}

func probeFirefoxProfile(certPath, profile string) StoreProbe {
	db := filepath.Join(profile, "cert9.db")
	store := "firefox-nss:" + filepath.Base(profile)
	if _, err := os.Stat(db); err != nil {
		// Profile directory exists but Firefox has never opened it (no
		// NSS DB). Don't fail — the user doesn't actually use this
		// profile, so the CA isn't needed there.
		return StoreProbe{Store: store, Skip: true, Note: "no cert9.db (profile never opened)"}
	}
	return StoreProbe{Store: store, Present: true, Note: db}
}
