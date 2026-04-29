package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func proxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Run the TLS-intercepting proxy (foreground)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented in this commit; see Task 14")
		},
	}
	return cmd
}
