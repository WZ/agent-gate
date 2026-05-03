package doctor

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"agent-gate/internal/agentdetect"
	"agent-gate/internal/ca"
	"agent-gate/internal/config"
	"agent-gate/internal/runtime"
)

type Status int

const (
	StatusOK Status = iota
	StatusWarn
	StatusFail
	StatusSkip
)

func (s Status) Glyph() string {
	switch s {
	case StatusOK:
		return "✓"
	case StatusWarn:
		return "!"
	case StatusFail:
		return "✗"
	case StatusSkip:
		return "–"
	default:
		return "?"
	}
}

type Result struct {
	ID      string
	Status  Status
	Detail  string
	FixHint string
}

func CheckCAFiles(caDir string) Result {
	cert := filepath.Join(caDir, "cert.pem")
	key := filepath.Join(caDir, "key.pem")
	certInfo, err := os.Stat(cert)
	if err != nil {
		return Result{ID: "ca-files", Status: StatusFail, Detail: "cert.pem not found",
			FixHint: "agent-gate init"}
	}
	keyInfo, err := os.Stat(key)
	if err != nil {
		return Result{ID: "ca-files", Status: StatusFail, Detail: "key.pem not found",
			FixHint: "agent-gate init"}
	}
	if !isWindowsRuntime() {
		// cert.pem allowed up to 0644; key.pem must be exactly 0600.
		if certInfo.Mode().Perm()&^0o644 != 0 {
			return Result{ID: "ca-files", Status: StatusWarn,
				Detail:  fmt.Sprintf("cert.pem mode %o is wider than 0644", certInfo.Mode().Perm()),
				FixHint: "chmod 0644 " + cert}
		}
		if keyInfo.Mode().Perm() != 0o600 {
			return Result{ID: "ca-files", Status: StatusFail,
				Detail:  fmt.Sprintf("key.pem mode %o is not 0600", keyInfo.Mode().Perm()),
				FixHint: "chmod 0600 " + key}
		}
	}
	return Result{ID: "ca-files", Status: StatusOK,
		Detail: fmt.Sprintf("cert.pem 0%o, key.pem 0600", certInfo.Mode().Perm())}
}

func CheckPortBindable(port int, label string) Result {
	addr := "127.0.0.1:" + strconv.Itoa(port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return Result{ID: label + "-port", Status: StatusFail,
			Detail:  fmt.Sprintf("%s: %v", addr, err),
			FixHint: "edit ~/.config/agent-gate/config.toml [ports] section, or stop the conflicting process"}
	}
	_ = ln.Close()
	return Result{ID: label + "-port", Status: StatusOK,
		Detail: addr + " bindable"}
}

func CheckDataDir(path string) Result {
	info, err := os.Stat(path)
	if err != nil {
		return Result{ID: "data-dir", Status: StatusFail,
			Detail:  fmt.Sprintf("%s missing", path),
			FixHint: "mkdir -p " + path + " && chmod 0700 " + path}
	}
	if !info.IsDir() {
		return Result{ID: "data-dir", Status: StatusFail,
			Detail: path + " is not a directory"}
	}
	if !isWindowsRuntime() && info.Mode().Perm() != 0o700 {
		return Result{ID: "data-dir", Status: StatusWarn,
			Detail:  fmt.Sprintf("mode %o (want 0700)", info.Mode().Perm()),
			FixHint: "chmod 0700 " + path}
	}
	probe := filepath.Join(path, ".doctor-probe")
	if err := os.WriteFile(probe, []byte("x"), 0o600); err != nil {
		return Result{ID: "data-dir", Status: StatusFail, Detail: "not writable: " + err.Error()}
	}
	_ = os.Remove(probe)
	return Result{ID: "data-dir", Status: StatusOK, Detail: path}
}

func CheckLockfile(lockPath string) Result {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return Result{ID: "lockfile", Status: StatusOK, Detail: "none"}
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return Result{ID: "lockfile", Status: StatusWarn, Detail: "unparseable lockfile",
			FixHint: "rm " + lockPath}
	}
	if runtime.ProcessAlive(pid) {
		return Result{ID: "lockfile", Status: StatusOK,
			Detail: fmt.Sprintf("held by PID %d", pid)}
	}
	return Result{ID: "lockfile", Status: StatusFail,
		Detail:  fmt.Sprintf("stale (PID %d gone)", pid),
		FixHint: "agent-gate stop  # or: rm " + lockPath}
}

func CheckHostListFile(path, label string) Result {
	info, err := os.Stat(path)
	if err != nil {
		return Result{ID: label + "-file", Status: StatusSkip, Detail: filepath.Base(path) + " absent (OK)"}
	}
	if !isWindowsRuntime() && info.Mode().Perm() != 0o600 {
		return Result{ID: label + "-file", Status: StatusWarn,
			Detail:  fmt.Sprintf("%s mode %o (want 0600)", filepath.Base(path), info.Mode().Perm()),
			FixHint: "chmod 0600 " + path}
	}
	return Result{ID: label + "-file", Status: StatusOK, Detail: filepath.Base(path)}
}

func CheckConfigValid(configPath string) Result {
	if _, err := os.Stat(configPath); err != nil {
		return Result{ID: "config-valid", Status: StatusFail,
			Detail: configPath + " missing", FixHint: "agent-gate init"}
	}
	if _, err := config.LoadFromFile(configPath); err != nil {
		return Result{ID: "config-valid", Status: StatusFail,
			Detail:  "TOML decode failed: " + err.Error(),
			FixHint: "$EDITOR " + configPath}
	}
	return Result{ID: "config-valid", Status: StatusOK, Detail: configPath}
}

func CheckAgentsDetected(agents []agentdetect.DetectedAgent) Result {
	if len(agents) == 0 {
		return Result{ID: "agents-detected", Status: StatusWarn,
			Detail:  "no agents detected on PATH or in env",
			FixHint: "install an agent (e.g., claude, codex) or pass --allow-host on init"}
	}
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		names = append(names, fmt.Sprintf("%s (%s)", a.Name, a.Source))
	}
	return Result{ID: "agents-detected", Status: StatusOK,
		Detail: strings.Join(names, ", ")}
}

func CheckCATrusted(installer ca.Installer, certPath string) []Result {
	probes := installer.ProbeAll(certPath)
	out := make([]Result, 0, len(probes))
	for _, p := range probes {
		var r Result
		r.ID = "ca-trusted-" + p.Store
		switch {
		case p.Err != nil:
			r.Status = StatusWarn
			r.Detail = fmt.Sprintf("%s: probe error: %v", p.Store, p.Err)
		case p.Present:
			r.Status = StatusOK
			r.Detail = p.Store
			if p.Note != "" {
				r.Detail += " — " + p.Note
			}
		default:
			r.Status = StatusFail
			r.Detail = p.Store + " missing"
			if p.Note != "" {
				r.Detail += " (" + p.Note + ")"
			}
			r.FixHint = "agent-gate cert install"
		}
		out = append(out, r)
	}
	return out
}

func isWindowsRuntime() bool {
	return os.PathSeparator == '\\'
}
