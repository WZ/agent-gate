package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"

	"agent-gate/internal/agentdetect"
	"agent-gate/internal/ca"
	"agent-gate/internal/initwizard"
	"agent-gate/internal/launcher"
	agruntime "agent-gate/internal/runtime"

	"github.com/spf13/cobra"
	"golang.org/x/net/idna"
	"golang.org/x/term"
)

func initCmd() *cobra.Command {
	var (
		configPath     string
		nonInteractive bool
		installCertStr string
		skipCertInst   bool
		regenerateCA   bool
		allowHosts     []string
		force          bool
		dryRun         bool
		printConfig    bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Bootstrap config + CA + agent detection + cert install",
		RunE: func(cmd *cobra.Command, args []string) error {
			configDir := filepath.Dir(configPath)
			caDir := filepath.Join(configDir, "ca")

			interactive := !nonInteractive && isInteractive()
			var prompter initwizard.Prompter
			if interactive {
				prompter = initwizard.HuhPrompter{}
			}

			installMode := initwizard.InstallCertAuto
			switch installCertStr {
			case "true":
				installMode = initwizard.InstallCertTrue
			case "false":
				installMode = initwizard.InstallCertFalse
			case "", "auto":
				installMode = initwizard.InstallCertAuto
			default:
				return fmt.Errorf("--install-cert must be auto|true|false (got %q)", installCertStr)
			}
			if skipCertInst {
				installMode = initwizard.InstallCertFalse
			}
			if !interactive && installMode == initwizard.InstallCertAuto {
				installMode = initwizard.InstallCertFalse
				fmt.Fprintln(os.Stderr, "init: non-interactive context — cert install skipped; run `agent-gate cert install` later.")
			}
			if !interactive && installMode == initwizard.InstallCertTrue {
				return fmt.Errorf("--install-cert=true requires a TTY for the sudo prompt; got non-interactive context. Re-run with --install-cert=false and run `agent-gate cert install` interactively later")
			}

			dataDir, _ := agruntime.DataDir()

			if !dryRun && !printConfig {
				if err := os.MkdirAll(dataDir, 0o700); err != nil {
					return fmt.Errorf("data dir: %w", err)
				}
				lockPath, _ := agruntime.LockfilePath()
				lock, err := agruntime.AcquireLockfile(lockPath)
				if err != nil {
					return err
				}
				defer lock.Release()

				// CA work happens AFTER lockfile acquisition so two concurrent
				// `init --regenerate-ca` invocations can't race on cert.pem/key.pem.
				if err := os.MkdirAll(caDir, 0o700); err != nil {
					return fmt.Errorf("ca dir: %w", err)
				}
				if regenerateCA {
					_ = os.Remove(filepath.Join(caDir, "cert.pem"))
					_ = os.Remove(filepath.Join(caDir, "key.pem"))
				}
				if _, err := ca.Ensure(caDir); err != nil {
					return fmt.Errorf("ca: %w", err)
				}
			}

			opts := initwizard.Options{
				ConfigPath:    configPath,
				ConfigDir:     configDir,
				DataDir:       dataDir,
				Installer:     ca.SmallstepInstaller{},
				Prompter:      prompter,
				Force:         force,
				AllowHosts:    allowHosts,
				InstallCert:   installMode,
				SkipSmokeTest: !interactive,
				DryRun:        dryRun,
				PrintConfig:   printConfig,
				Detector: func() []agentdetect.DetectedAgent {
					return agentdetect.Run(agentdetect.Config{
						PathLookup: exec.LookPath,
						EnvGetter:  os.Getenv,
						IDNLookup:  idna.Lookup.ToASCII,
					})
				},
			}

			if err := initwizard.Run(opts); err != nil {
				if errors.Is(err, initwizard.ErrConfigExists) {
					return fmt.Errorf("agent-gate init: config already exists at %s. Re-run with --force to overwrite, or run `agent-gate doctor` to validate the existing install", configPath)
				}
				return err
			}

			if goruntime.GOOS == "windows" && !dryRun && !printConfig {
				if err := launcher.InstallWFP(); err != nil {
					return fmt.Errorf("WFP install (run as admin): %w", err)
				}
			}
			return nil
		},
	}
	defaultConfig, _ := agruntime.ConfigPath()
	cmd.Flags().StringVar(&configPath, "config", defaultConfig, "Path to config.toml")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Skip prompts; use defaults / flags")
	cmd.Flags().StringVar(&installCertStr, "install-cert", "auto", "auto|true|false")
	cmd.Flags().BoolVar(&skipCertInst, "skip-cert-install", false, "Equivalent to --install-cert=false")
	cmd.Flags().BoolVar(&regenerateCA, "regenerate-ca", false, "Force-regenerate the local CA (rotates the cert)")
	cmd.Flags().StringSliceVar(&allowHosts, "allow-host", nil, "Seed allowlist with HOST (repeatable; replaces detection)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing config.toml")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print planned writes; change nothing")
	cmd.Flags().BoolVar(&printConfig, "print-config", false, "Emit the would-be config.toml on stdout; exit 0")
	return cmd
}

// isInteractive returns true when stdin AND stdout are TTYs and no env var
// forces non-interactive (CI=true, NONINTERACTIVE=1, DEBIAN_FRONTEND=noninteractive).
func isInteractive() bool {
	if v := os.Getenv("CI"); v == "true" {
		return false
	}
	if v := os.Getenv("NONINTERACTIVE"); v == "1" {
		return false
	}
	if v := os.Getenv("DEBIAN_FRONTEND"); v == "noninteractive" {
		return false
	}
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}
