package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// tryResolveAppURL returns the app URL if configured, or "" if not available.
// Unlike resolveAppURL, it never calls os.Exit.
func tryResolveAppURL(cmd *cobra.Command) string {
	for _, key := range []string{"MULTICA_APP_URL", "FRONTEND_ORIGIN"} {
		if val := strings.TrimSpace(os.Getenv(key)); val != "" {
			return strings.TrimRight(val, "/")
		}
	}
	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err == nil && cfg.AppURL != "" {
		return strings.TrimRight(cfg.AppURL, "/")
	}
	return ""
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate and set up workspaces",
	Long:  "Log in to Multica, then automatically discover and watch all your workspaces.",
	RunE:  runLogin,
}

func init() {
	loginCmd.Flags().Bool("token", false, "Authenticate by pasting a personal access token")
	loginCmd.Flags().String(callbackHostFlag, "", "Host the OAuth callback URL points at (auto-detected from the server's route when empty). Use this for reverse-proxy / FQDN setups where auto-detection picks the wrong interface.")
}

func runLogin(cmd *cobra.Command, args []string) error {
	// Run the standard auth login flow.
	if err := runAuthLogin(cmd, args); err != nil {
		return err
	}

	// Auto-discover and watch all workspaces.
	if err := autoWatchWorkspaces(cmd); err != nil {
		fmt.Fprintf(os.Stderr, "\nCould not auto-configure workspaces: %v\n", err)
		fmt.Fprintf(os.Stderr, "Run 'multica workspace list' and 'multica workspace watch <id>' to set up manually.\n")
		return nil
	}

	fmt.Fprintf(os.Stderr, "\n→ Run 'multica daemon start' to start your local agent runtime.\n")
	return nil
}

func autoWatchWorkspaces(cmd *cobra.Command) error {
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated")
	}

	client := cli.NewAPIClient(serverURL, "", token)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var workspaces []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	if len(workspaces) == 0 {
		var err error
		workspaces, err = waitForWorkspaceCreation(cmd, client)
		if err != nil {
			return err
		}
		if len(workspaces) == 0 {
			fmt.Fprintln(os.Stderr, "\nNo workspaces found.")
			return nil
		}
	}

	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return err
	}

	// Set default workspace if not set.
	if cfg.WorkspaceID == "" {
		cfg.WorkspaceID = workspaces[0].ID
	}

	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\nFound %d workspace(s):\n", len(workspaces))
	for _, ws := range workspaces {
		fmt.Fprintf(os.Stderr, "  • %s (%s)\n", ws.Name, ws.ID)
	}

	return nil
}

// waitForWorkspaceCreation opens the web workspace-creation page and polls
// until the user creates a workspace, returning the new workspace list.
func waitForWorkspaceCreation(cmd *cobra.Command, client *cli.APIClient) ([]struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}, error) {
	appURL := tryResolveAppURL(cmd)
	if appURL == "" {
		// No app URL available (e.g. token login without prior setup).
		// Can't open the browser — tell the user to create a workspace manually.
		fmt.Fprintln(os.Stderr, "\nNo workspaces found.")
		fmt.Fprintln(os.Stderr, "Create a workspace in the web dashboard, then run 'multica login' again.")
		return nil, nil
	}

	createWorkspaceURL := appURL + "/workspaces/new"

	fmt.Fprintln(os.Stderr, "\nNo workspaces found. Opening workspace creation in your browser...")
	if err := openBrowser(createWorkspaceURL); err != nil {
		fmt.Fprintf(os.Stderr, "Could not open browser automatically.\n")
	}
	fmt.Fprintf(os.Stderr, "If the browser didn't open, visit:\n  %s\n", createWorkspaceURL)
	fmt.Fprintln(os.Stderr, "\nWaiting for workspace creation...")

	// Poll until a workspace appears or timeout (5 minutes).
	const pollInterval = 2 * time.Second
	const pollTimeout = 5 * time.Minute
	deadline := time.Now().Add(pollTimeout)

	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var workspaces []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		err := client.GetJSON(ctx, "/api/workspaces", &workspaces)
		cancel()

		if err != nil {
			continue // transient error, keep polling
		}
		if len(workspaces) > 0 {
			return workspaces, nil
		}
	}

	return nil, fmt.Errorf("timed out waiting for workspace creation")
}
