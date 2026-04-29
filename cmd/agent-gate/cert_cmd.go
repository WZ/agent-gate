package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"agent-gate/internal/ca"
	"github.com/spf13/cobra"
)

func certCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cert", Short: "Manage the local CA"}
	cmd.AddCommand(certInstallCmd())
	cmd.AddCommand(certPathCmd())
	return cmd
}

func certInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install the local root CA into the system trust store",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, _ := os.UserHomeDir()
			caDir := filepath.Join(home, ".config", "agent-gate", "ca")
			root, err := ca.Ensure(caDir)
			if err != nil {
				return err
			}
			certPath := filepath.Join(caDir, "cert.pem")
			fmt.Fprintf(os.Stderr, "CA cert at: %s\n", certPath)
			fmt.Fprintf(os.Stderr, "Subject: %s\n", root.Cert.Subject)

			switch runtime.GOOS {
			case "darwin":
				return installMacOS(certPath)
			default:
				return printManualInstructions(certPath)
			}
		},
	}
}

func certPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the path to the local CA cert",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, _ := os.UserHomeDir()
			caDir := filepath.Join(home, ".config", "agent-gate", "ca")
			if _, err := ca.Ensure(caDir); err != nil {
				return err
			}
			fmt.Println(filepath.Join(caDir, "cert.pem"))
			return nil
		},
	}
}

func installMacOS(certPath string) error {
	c := exec.Command(
		"sudo", "security", "add-trusted-cert",
		"-d",
		"-r", "trustRoot",
		"-k", "/Library/Keychains/System.keychain",
		certPath,
	)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	fmt.Fprintln(os.Stderr, "Running: "+c.String())
	if err := c.Run(); err != nil {
		return fmt.Errorf("security add-trusted-cert: %w", err)
	}
	fmt.Fprintln(os.Stderr, "✓ CA installed in System keychain.")
	return nil
}

func printManualInstructions(certPath string) error {
	fmt.Fprintf(os.Stderr, `
Automated install not yet implemented for %s. Manual install:

  - Linux: copy %s to /usr/local/share/ca-certificates/agent-gate.crt and run
           sudo update-ca-certificates
  - Windows: certutil -addstore -f Root %s    (run as administrator)

`, runtime.GOOS, certPath, certPath)
	return nil
}
