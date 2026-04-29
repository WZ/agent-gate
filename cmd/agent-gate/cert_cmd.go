package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func certCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "cert", Short: "Manage the local CA"}
	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Install the local CA into the system trust store",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented in this commit; see Task 15")
		},
	})
	return cmd
}
