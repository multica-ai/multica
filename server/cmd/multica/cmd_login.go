package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate and set up workspaces",
	Long:  "Log in to Multica, then automatically discover and watch all your workspaces.",
	RunE:  runLogin,
}

func init() {
	loginCmd.Flags().Bool("token", false, "Authenticate by pasting a personal access token")
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
		fmt.Fprintln(os.Stderr, "\nNo workspaces found.")
		return nil
	}

	profile := resolveProfile(cmd)
	var added int
	if err := cli.UpdateCLIConfigForProfile(profile, func(cfg *cli.CLIConfig) error {
		for _, ws := range workspaces {
			if cfg.AddWatchedWorkspace(ws.ID, ws.Name) {
				added++
			}
		}
		if cfg.WorkspaceID == "" {
			cfg.WorkspaceID = workspaces[0].ID
		}
		return nil
	}); err != nil {
		return err
	}

	if added > 0 {
		fmt.Fprintf(os.Stderr, "\nWatching %d workspace(s):\n", len(workspaces))
		for _, ws := range workspaces {
			fmt.Fprintf(os.Stderr, "  • %s (%s)\n", ws.Name, ws.ID)
		}
	} else {
		fmt.Fprintf(os.Stderr, "\nAll %d workspace(s) already watched.\n", len(workspaces))
	}

	return nil
}
