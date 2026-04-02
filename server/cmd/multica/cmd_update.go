package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update multica to the latest version",
	RunE:  runUpdate,
}

func runUpdate(_ *cobra.Command, _ []string) error {
	fmt.Fprintf(os.Stderr, "Current version: %s (commit: %s)\n", version, commit)

	// Check latest version from GitHub.
	latest, err := cli.FetchLatestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not check latest version: %v\n", err)
	} else {
		latestVer := strings.TrimPrefix(latest.TagName, "v")
		currentVer := strings.TrimPrefix(version, "v")
		if currentVer == latestVer {
			fmt.Fprintln(os.Stderr, "Already up to date.")
			return nil
		}
		fmt.Fprintf(os.Stderr, "Latest version:  %s\n\n", latest.TagName)
	}

	// Detect installation method and update accordingly.
	if cli.IsBrewInstall() {
		fmt.Fprintln(os.Stderr, "Updating via Homebrew...")
		output, err := cli.UpdateViaBrew()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", output)
			return fmt.Errorf("brew upgrade failed: %w\nYou can try manually: brew upgrade multica-ai/tap/multica", err)
		}
		fmt.Fprintln(os.Stderr, "Update complete.")
		return nil
	}

	// Not installed via brew — show manual instructions.
	fmt.Fprintln(os.Stderr, "multica was not installed via Homebrew.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "To install via Homebrew (recommended):")
	fmt.Fprintln(os.Stderr, "  brew install multica-ai/tap/multica")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Or download the latest release from:")
	fmt.Fprintln(os.Stderr, "  https://github.com/multica-ai/multica/releases/latest")
	return nil
}
