package main

import (
	"bytes"
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

var repoCheckoutRef string

var repoRefreshCmd = &cobra.Command{
	Use:   "refresh <url>",
	Short: "Force the repocache to fetch the latest refs for a repository",
	Long: `Forces an immediate fetch of the remote into the workspace's cached bare clone.

Use this when a commit pushed within the last minute is not yet visible in
` + "`multica repo checkout`" + ` or via ` + "`git fetch`" + ` from within a checked-out worktree.
The repocache otherwise refreshes on a fixed interval (default 60s), so most
runs do not need this command.

In a worker pod (controller-mode), the command proxies to the cluster
repocache server's admin endpoint. In daemon mode, it asks the local daemon
to run a ` + "`git fetch`" + ` against its bare clone directly.`,
	Args: exactArgs(1),
	RunE: runRepoRefresh,
}

func init() {
	repoCheckoutCmd.Flags().StringVar(&repoCheckoutRef, "ref", "", "branch, tag, or commit to check out instead of the remote default branch")
	repoCmd.AddCommand(repoCheckoutCmd)
	repoCmd.AddCommand(repoRefreshCmd)
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

func runRepoRefresh(cmd *cobra.Command, args []string) error {
	repoURL := args[0]

	daemonPort := os.Getenv("MULTICA_DAEMON_PORT")
	if daemonPort == "" {
		return fmt.Errorf("MULTICA_DAEMON_PORT not set (this command is intended to be run by an agent inside a daemon task)")
	}

	reqBody := map[string]string{
		"url":          repoURL,
		"workspace_id": os.Getenv("MULTICA_WORKSPACE_ID"),
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	// Fetches against a slow remote can take a while; allow up to 5 minutes
	// like the checkout path does.
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Post(
		fmt.Sprintf("http://127.0.0.1:%s/repo/refresh", daemonPort),
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("refresh failed: %s", strings.TrimSpace(string(body)))
	}

	fmt.Fprintf(os.Stderr, "Refreshed %s\n", repoURL)
	return nil
}
