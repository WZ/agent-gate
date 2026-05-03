package runtime

import (
	"os"
	"path/filepath"
	"runtime"
)

// ConfigDir returns the agent-gate config directory.
//
// Linux:   $XDG_CONFIG_HOME/agent-gate (default $HOME/.config/agent-gate)
// macOS:   $HOME/.config/agent-gate
// Windows: %APPDATA%/agent-gate
func ConfigDir() (string, error) {
	if runtime.GOOS == "linux" {
		if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
			return filepath.Join(x, "agent-gate"), nil
		}
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "agent-gate"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "agent-gate"), nil
}

// DataDir returns the agent-gate persistent data directory.
//
// Linux:   $XDG_DATA_HOME/agent-gate (default $HOME/.local/share/agent-gate)
// macOS:   $HOME/Library/Application Support/agent-gate
// Windows: %LOCALAPPDATA%/agent-gate
func DataDir() (string, error) {
	switch runtime.GOOS {
	case "linux":
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			return filepath.Join(x, "agent-gate"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "agent-gate"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "agent-gate"), nil
	case "windows":
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "agent-gate"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "agent-gate"), nil
}

// CacheDir returns the agent-gate cache directory.
func CacheDir() (string, error) {
	if runtime.GOOS == "linux" {
		if x := os.Getenv("XDG_CACHE_HOME"); x != "" {
			return filepath.Join(x, "agent-gate"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "agent-gate"), nil
}

// ConfigPath returns ConfigDir + "config.toml".
func ConfigPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "config.toml"), nil
}

// CADir returns ConfigDir + "ca".
func CADir() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "ca"), nil
}

// AllowlistPath / DenylistPath / PassthroughPath return the canonical paths
// for the three host-list files. agent-gate v0.6.0+ does NOT honor a custom
// path from config.toml; lists always live under ConfigDir.
func AllowlistPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "allowlist.txt"), nil
}

func DenylistPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "denylist.txt"), nil
}

func PassthroughPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "passthrough.txt"), nil
}

// DismissalsPath returns ConfigDir + "dismissals.json".
func DismissalsPath() (string, error) {
	d, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "dismissals.json"), nil
}

// LockfilePath returns DataDir + "agent-gate.lock".
func LockfilePath() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "agent-gate.lock"), nil
}

// DashboardLockfilePath returns DataDir + "agent-gate-dashboard.lock".
// Used by `agent-gate dashboard` standalone (not by `agent-gate run`).
func DashboardLockfilePath() (string, error) {
	d, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "agent-gate-dashboard.lock"), nil
}
