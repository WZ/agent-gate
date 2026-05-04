package main

import (
	"fmt"
	"os"
	"path/filepath"

	"agent-gate/internal/ca"
	agruntime "agent-gate/internal/runtime"

	"github.com/spf13/cobra"
)

func certCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cert", Short: "Manage the local CA"}
	cmd.AddCommand(certInstallCmd())
	cmd.AddCommand(certUninstallCmd())
	cmd.AddCommand(certPathCmd())
	return cmd
}

func certInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install the local root CA into all available trust stores",
		RunE: func(cmd *cobra.Command, args []string) error {
			caDir, err := agruntime.CADir()
			if err != nil {
				return err
			}
			if _, err := ca.Ensure(caDir); err != nil {
				return err
			}
			certPath := filepath.Join(caDir, "cert.pem")
			fmt.Fprintf(os.Stderr, "CA cert: %s\n", certPath)

			installer := ca.SmallstepInstaller{}
			if err := installer.InstallFile(certPath); err != nil {
				return fmt.Errorf("install: %w", err)
			}
			for _, p := range installer.ProbeAll(certPath) {
				glyph := "✓"
				if !p.Present {
					glyph = "✗"
				}
				fmt.Fprintf(os.Stderr, "  %s  %s  %s\n", glyph, p.Store, p.Note)
			}
			return nil
		},
	}
}

func certUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove the local root CA from all trust stores",
		RunE: func(cmd *cobra.Command, args []string) error {
			caDir, err := agruntime.CADir()
			if err != nil {
				return err
			}
			certPath := filepath.Join(caDir, "cert.pem")
			if _, err := os.Stat(certPath); err != nil {
				return fmt.Errorf("no CA at %s", certPath)
			}
			installer := ca.SmallstepInstaller{}
			if err := installer.UninstallFile(certPath); err != nil {
				return fmt.Errorf("uninstall: %w", err)
			}
			fmt.Fprintln(os.Stderr, "✓ CA removed from trust stores. (cert.pem and key.pem remain on disk; delete manually if you want them gone.)")
			return nil
		},
	}
}

func certPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the path to the local CA cert",
		RunE: func(cmd *cobra.Command, args []string) error {
			caDir, err := agruntime.CADir()
			if err != nil {
				return err
			}
			if _, err := ca.Ensure(caDir); err != nil {
				return err
			}
			fmt.Println(filepath.Join(caDir, "cert.pem"))
			return nil
		},
	}
}
