package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "One-command setup: configure, authenticate, and start the daemon",
	Long: `Detects a local Multica server, configures the CLI, authenticates via browser,
and starts the agent daemon — all in one step.

Use --local to skip auto-detection and force local mode.`,
	RunE: runSetup,
}

func init() {
	setupCmd.Flags().Bool("local", false, "Force local mode (skip server auto-detection)")
	setupCmd.Flags().Int("port", 8080, "Backend server port (for local mode)")
	setupCmd.Flags().Int("frontend-port", 3000, "Frontend port (for local mode)")
}

func runSetup(cmd *cobra.Command, args []string) error {
	forceLocal, _ := cmd.Flags().GetBool("local")
	port, _ := cmd.Flags().GetInt("port")
	frontendPort, _ := cmd.Flags().GetInt("frontend-port")

	profile := resolveProfile(cmd)

	isLocal := forceLocal
	if !forceLocal {
		// Auto-detect a local server on the default port.
		isLocal = probeLocalServer(port)
	}

	if isLocal {
		fmt.Fprintln(os.Stderr, "Detected local Multica server.")

		cfg, _ := cli.LoadCLIConfigForProfile(profile)
		cfg.AppURL = fmt.Sprintf("http://localhost:%d", frontendPort)
		cfg.ServerURL = fmt.Sprintf("http://localhost:%d", port)
		if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		fmt.Fprintf(os.Stderr, "  app_url:    %s\n", cfg.AppURL)
		fmt.Fprintf(os.Stderr, "  server_url: %s\n", cfg.ServerURL)
	} else if !forceLocal {
		fmt.Fprintln(os.Stderr, "No local server detected — using Multica Cloud (https://multica.ai).")
	}

	// Authenticate.
	fmt.Fprintln(os.Stderr, "")
	if err := runLogin(cmd, args); err != nil {
		return err
	}

	// Start daemon in background.
	fmt.Fprintln(os.Stderr, "\nStarting daemon...")
	if err := runDaemonBackground(cmd); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	fmt.Fprintln(os.Stderr, "\n✓ Setup complete! Your machine is now connected to Multica.")
	return nil
}

// probeLocalServer checks whether a Multica backend is running on localhost.
func probeLocalServer(port int) bool {
	url := fmt.Sprintf("http://localhost:%d/health", port)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
