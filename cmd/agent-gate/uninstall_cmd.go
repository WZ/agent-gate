package main

import (
	"fmt"
	"os"
	"runtime"

	"agent-gate/internal/launcher"
	"github.com/spf13/cobra"
)

func uninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "(Windows) Remove the WFP provider and sublayer",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "windows" {
				return fmt.Errorf("`agent-gate uninstall` is windows-only; nothing to do on %s", runtime.GOOS)
			}
			if err := launcher.UninstallWFP(); err != nil {
				return fmt.Errorf("WFP uninstall (run as admin): %w", err)
			}
			fmt.Fprintln(os.Stderr, "agent-gate uninstall: complete")
			return nil
		},
	}
	return cmd
}
