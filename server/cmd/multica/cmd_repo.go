package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Work with repositories",
}

var repoCheckoutCmd = &cobra.Command{
	Use:   "checkout <url>",
	Short: "Check out a repository into the working directory",
	Long:  "Creates a git worktree from the daemon's bare clone cache. Used by agents to check out repos on demand.",
	Args:  exactArgs(1),
	RunE:  runRepoCheckout,
}

var repoApproveCmd = &cobra.Command{
	Use:   "approve <url>[,<url>...]",
	Short: "Mark workspace repos as approved so projects can attach them",
	Long:  "Promotes workspace-level repositories from 'pending' to 'approved'. Accepts a comma-separated list of URLs. Defaults to the active workspace (see `multica config workspace_id`); pass --workspace-id to target a different workspace.",
	Args:  exactArgs(1),
	RunE:  runRepoApprove,
}

var (
	repoCheckoutRef      string
	repoApproveWorkspace string
)

func init() {
	repoCheckoutCmd.Flags().StringVar(&repoCheckoutRef, "ref", "", "branch, tag, or commit to check out instead of the remote default branch")
	repoApproveCmd.Flags().StringVar(&repoApproveWorkspace, "workspace-id", "", "workspace UUID to approve the repo in (defaults to the active workspace)")
	repoCmd.AddCommand(repoCheckoutCmd)
	repoCmd.AddCommand(repoApproveCmd)
}

func runRepoApprove(cmd *cobra.Command, args []string) error {
	repoURLs := make([]string, 0)
	for _, raw := range strings.Split(args[0], ",") {
		if u := strings.TrimSpace(raw); u != "" {
			repoURLs = append(repoURLs, u)
		}
	}
	if len(repoURLs) == 0 {
		return fmt.Errorf("at least one repo url is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	workspaceID := repoApproveWorkspace
	if workspaceID == "" {
		workspaceID = client.WorkspaceID
	}
	if workspaceID == "" {
		return fmt.Errorf("no active workspace; set one with `multica config workspace_id <uuid>` or pass --workspace-id")
	}

	for _, repoURL := range repoURLs {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		body := map[string]string{"url": repoURL}
		var result map[string]any
		err := client.PostJSON(ctx, "/api/workspaces/"+workspaceID+"/repos/approve", body, &result)
		cancel()
		if err != nil {
			return fmt.Errorf("approve repo %s: %w", repoURL, err)
		}
		fmt.Fprintf(os.Stderr, "Approved %s in workspace %s.\n", repoURL, truncateID(workspaceID))
	}
	return nil
}

func runRepoCheckout(cmd *cobra.Command, args []string) error {
	repoURL := args[0]

	daemonPort := os.Getenv("MULTICA_DAEMON_PORT")
	if daemonPort == "" {
		return fmt.Errorf("MULTICA_DAEMON_PORT not set (this command is intended to be run by an agent inside a daemon task)")
	}

	workspaceID := os.Getenv("MULTICA_WORKSPACE_ID")
	agentName := os.Getenv("MULTICA_AGENT_NAME")
	taskID := os.Getenv("MULTICA_TASK_ID")

	// Use current working directory as the checkout target.
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	reqBody := map[string]string{
		"url":          repoURL,
		"workspace_id": workspaceID,
		"workdir":      workDir,
		"ref":          repoCheckoutRef,
		"agent_name":   agentName,
		"task_id":      taskID,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Post(
		fmt.Sprintf("http://127.0.0.1:%s/repo/checkout", daemonPort),
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("checkout failed: %s", string(body))
	}

	var result struct {
		Path       string `json:"path"`
		BranchName string `json:"branch_name"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	fmt.Fprintf(os.Stdout, "%s\n", result.Path)
	fmt.Fprintf(os.Stderr, "Checked out %s → %s (branch: %s)\n", repoURL, result.Path, result.BranchName)

	return nil
}
