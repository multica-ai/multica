package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

func init() {
	versionCmd.Flags().String("output", "text", "Output format: text or json")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	RunE:  runVersion,
}

func runVersion(cmd *cobra.Command, _ []string) error {
	output, _ := cmd.Flags().GetString("output")
	exePath, _ := os.Executable()

	if output == "json" {
		info := map[string]string{
			"version":         version,
			"commit":          commit,
			"date":            date,
			"go":              runtime.Version(),
			"os":              runtime.GOOS,
			"arch":            runtime.GOARCH,
			"executable_path": exePath,
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "multica %s (commit: %s, built: %s)\n", version, commit, date)
	fmt.Fprintf(cmd.OutOrStdout(), "go: %s, os/arch: %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
	if exePath != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "executable: %s\n", exePath)
	}
	return nil
}
