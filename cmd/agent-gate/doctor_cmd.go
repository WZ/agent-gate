package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"agent-gate/internal/agentdetect"
	"agent-gate/internal/ca"
	"agent-gate/internal/config"
	"agent-gate/internal/doctor"
	agruntime "agent-gate/internal/runtime"

	"github.com/spf13/cobra"
	"golang.org/x/net/idna"
)

// errDoctorFailed is returned when one or more checks reported StatusFail.
// main() catches this and exits 1 without re-printing the (already-rendered) report.
var errDoctorFailed = errors.New("doctor: one or more checks failed")

func doctorCmd() *cobra.Command {
	var (
		configPath string
		autoRepair string
		jsonOut    bool
	)
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate the agent-gate install; suggest or apply repairs",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir := filepath.Dir(configPath)
			caDir := filepath.Join(configDir, "ca")

			agents := agentdetect.Run(agentdetect.Config{
				PathLookup: exec.LookPath,
				EnvGetter:  os.Getenv,
				IDNLookup:  idna.Lookup.ToASCII,
			})

			results := []doctor.Result{}
			results = append(results, doctor.CheckConfigValid(configPath))
			results = append(results, doctor.CheckCAFiles(caDir))

			proxyPort, dashPort := 8888, 7878
			dataDir, _ := agruntime.DataDir()
			if cfg, err := config.LoadFromFile(configPath); err == nil {
				if cfg.Ports.Proxy != 0 {
					proxyPort = cfg.Ports.Proxy
				}
				if cfg.Ports.Dashboard != 0 {
					dashPort = cfg.Ports.Dashboard
				}
				if cfg.Storage.DataDir != "" {
					dataDir = cfg.Storage.DataDir
				}
			}

			lockPath, _ := agruntime.LockfilePath()
			results = append(results, doctor.CheckPortBindable(proxyPort, "proxy", lockPath))
			results = append(results, doctor.CheckPortBindable(dashPort, "dashboard", lockPath))
			results = append(results, doctor.CheckLockfile(lockPath))

			results = append(results, doctor.CheckDataDir(dataDir))

			results = append(results, doctor.CheckHostListFile(filepath.Join(configDir, "allowlist.txt"), "allowlist"))
			results = append(results, doctor.CheckHostListFile(filepath.Join(configDir, "denylist.txt"), "denylist"))
			results = append(results, doctor.CheckHostListFile(filepath.Join(configDir, "passthrough.txt"), "passthrough"))
			results = append(results, doctor.CheckHostListFile(filepath.Join(configDir, "dismissals.json"), "dismissals"))

			installer := ca.SmallstepInstaller{}
			certPath := filepath.Join(caDir, "cert.pem")
			results = append(results, doctor.CheckCATrusted(installer, certPath)...)

			results = append(results, doctor.CheckAgentsDetected(agents))
			results = append(results, doctor.CheckCodexWebSocketPinning(agents))

			if autoRepair != "" {
				var mode doctor.Mode
				switch autoRepair {
				case "safe":
					mode = doctor.ModeSafe
				case "aggressive":
					mode = doctor.ModeAggressive
				default:
					return fmt.Errorf("--auto-repair must be safe|aggressive (got %q)", autoRepair)
				}
				doctor.Repair(doctor.RepairOpts{
					Mode:      mode,
					Installer: installer,
					ConfigDir: configDir,
					CertPath:  certPath,
					LockPath:  lockPath,
				}).Apply(results)
			}

			rep := doctor.Report{Results: results}
			out := cmd.OutOrStdout()
			if jsonOut {
				if err := rep.WriteJSON(out); err != nil {
					return err
				}
			} else {
				rep.WriteHuman(out)
			}

			for _, r := range results {
				if r.Status == doctor.StatusFail {
					return errDoctorFailed
				}
			}
			return nil
		},
		SilenceErrors: true,
	}
	defaultConfig, _ := agruntime.ConfigPath()
	cmd.Flags().StringVar(&configPath, "config", defaultConfig, "Path to config.toml")
	cmd.Flags().StringVar(&autoRepair, "auto-repair", "", "safe|aggressive (default: report only)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit machine-readable JSON")
	return cmd
}
