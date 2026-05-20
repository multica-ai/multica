package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update multica to the latest version",
	RunE:  runUpdate,
}

func runUpdate(_ *cobra.Command, _ []string) error {
	fmt.Fprintf(os.Stderr, "Current version: %s (commit: %s, built: %s)\n", version, commit, date)

	installedPath, err := cli.ResolveInstalledBinaryPath()
	if err == nil {
		fmt.Fprintf(os.Stderr, "Install path:     %s\n", installedPath)
	}

	if cli.IsBrewInstall() {
		fmt.Fprintln(os.Stderr, "Updating via Homebrew...")
		output, err := cli.UpdateViaBrew()
		if err != nil {
			return fmt.Errorf("update failed: %w", err)
		}
		fmt.Fprintf(os.Stderr, "%s\nUpdate complete.\n", output)
		return nil
	}

	latest, err := cli.FetchLatestManifestRelease()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not check latest version: %v\n", err)
	} else {
		shouldUpdate, cmpErr := cli.ShouldUpdate(version, latest)
		if cmpErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not compare versions: %v\n", cmpErr)
		} else if !shouldUpdate {
			fmt.Fprintln(os.Stderr, "Already up to date.")
			return nil
		}
		fmt.Fprintf(os.Stderr, "Latest version:  %s\n\n", latest.Version)
	}

	if latest == nil {
		return fmt.Errorf("could not determine latest version from update manifest")
	}
	targetVersion := latest.Version
	fmt.Fprintf(os.Stderr, "Downloading %s from configured update manifest...\n", targetVersion)
	output, err := cli.UpdateViaManifestDownload(targetVersion)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Manifest download failed (%v), falling back to GitHub Release...\n", err)
		output, err = cli.UpdateViaDownload(targetVersion)
		if err != nil {
			return fmt.Errorf("update failed: %w", err)
		}
	}
	fmt.Fprintf(os.Stderr, "%s\nUpdate complete.\n", output)
	return nil
}
