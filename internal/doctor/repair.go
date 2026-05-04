package doctor

import (
	"fmt"
	"os"
	"strings"

	"agent-gate/internal/ca"
)

type Mode int

const (
	ModeSafe Mode = iota
	ModeAggressive
)

type RepairOpts struct {
	Mode      Mode
	Installer ca.Installer
	ConfigDir string
	CertPath  string
	LockPath  string
}

type Repairer struct{ opts RepairOpts }

func Repair(opts RepairOpts) *Repairer { return &Repairer{opts: opts} }

func (r *Repairer) Apply(results []Result) {
	for _, res := range results {
		if res.Status == StatusOK || res.Status == StatusSkip {
			continue
		}
		switch {
		case strings.HasPrefix(res.ID, "ca-trusted-"):
			r.repairTrust(res)
		case res.ID == "data-dir" || strings.HasSuffix(res.ID, "-file") || res.ID == "ca-files":
			r.repairChmod(res)
		}
	}
}

func (r *Repairer) repairTrust(res Result) {
	if r.opts.Mode != ModeAggressive || r.opts.Installer == nil || r.opts.CertPath == "" {
		return
	}
	if err := r.opts.Installer.InstallFile(r.opts.CertPath); err != nil {
		fmt.Fprintf(os.Stderr, "doctor: trust repair failed: %v\n", err)
	}
}

func (r *Repairer) repairChmod(res Result) {
	parts := strings.Fields(res.FixHint)
	if len(parts) < 3 || parts[0] != "chmod" {
		return
	}
	mode, err := parsePermArg(parts[1])
	if err != nil {
		return
	}
	path := strings.Join(parts[2:], " ")
	if r.opts.ConfigDir != "" && !strings.HasPrefix(path, r.opts.ConfigDir) {
		// Sanity gate: only chmod paths under our config dir.
		// In tests, ConfigDir is set to t.TempDir() which contains the target.
		return
	}
	if err := os.Chmod(path, mode); err != nil {
		fmt.Fprintf(os.Stderr, "doctor: chmod %s failed: %v\n", path, err)
	}
}

func parsePermArg(s string) (os.FileMode, error) {
	var mode os.FileMode
	for _, c := range s {
		if c < '0' || c > '7' {
			return 0, fmt.Errorf("bad mode %q", s)
		}
		mode = mode*8 + os.FileMode(c-'0')
	}
	return mode, nil
}
