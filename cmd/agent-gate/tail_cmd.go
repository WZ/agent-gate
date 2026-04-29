package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func tailCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tail",
		Short: "Tail captured events as they arrive",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented in this commit; see Task 16")
		},
	}
}
