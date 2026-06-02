package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update multica to the latest version",
	RunE:  runUpdate,
}

func runUpdate(_ *cobra.Command, _ []string) error {
	fmt.Fprintf(os.Stderr, "Current version: %s (commit: %s, built: %s)\n", version, commit, date)
	return fmt.Errorf("self-hosted: updates managed manually — update the binary or Docker image directly")
}
