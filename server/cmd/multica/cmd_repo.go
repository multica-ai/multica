package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

// CEREBRO-PATCH(daemon-repo-grants): FIR-2512 — pre-flight repo capability check.
var repoCheckCmd = &cobra.Command{
	Use:   "check <url>",
	Short: "Check whether the current agent may access a repository",
	Long:  "Pre-flight check: asks the server whether the agent holds repo.checkout access for the given URL before attempting a checkout.",
	Args:  exactArgs(1),
	RunE:  runRepoCheck,
}

var repoCheckoutRef string

func init() {
	repoCheckoutCmd.Flags().StringVar(&repoCheckoutRef, "ref", "", "branch, tag, or commit to check out instead of the remote default branch")
	repoCmd.AddCommand(repoCheckoutCmd)
	repoCmd.AddCommand(repoCheckCmd)
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
	// CEREBRO-PATCH(daemon-repo-grants): FIR-2512 — pass agent/project for grants check.
	agentID := os.Getenv("MULTICA_AGENT_ID")
	projectID := os.Getenv("MULTICA_PROJECT_ID")

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
		"agent_id":     agentID,
		"project_id":   projectID,
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

// runRepoCheck performs a pre-flight grants check: asks the server whether the
// current agent has repo.checkout access for the given URL, so agents can
// request access proactively instead of hitting a blocked checkout.
// CEREBRO-PATCH(daemon-repo-grants): FIR-2512
func runRepoCheck(cmd *cobra.Command, args []string) error {
	repoURL := args[0]

	serverURL := os.Getenv("MULTICA_SERVER_URL")
	if serverURL == "" {
		return fmt.Errorf("MULTICA_SERVER_URL not set")
	}
	token := os.Getenv("MULTICA_TOKEN")
	workspaceID := os.Getenv("MULTICA_WORKSPACE_ID")
	agentID := os.Getenv("MULTICA_AGENT_ID")
	projectID := os.Getenv("MULTICA_PROJECT_ID")

	if workspaceID == "" {
		return fmt.Errorf("MULTICA_WORKSPACE_ID not set")
	}

	reqBody := map[string]string{
		"url":        repoURL,
		"capability": "repo.checkout",
		"agent_id":   agentID,
		"project_id": projectID,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/api/daemon/workspaces/%s/repo/check", serverURL, workspaceID),
		bytes.NewReader(data),
	)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("check failed (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Allowed  bool   `json:"allowed"`
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if result.Allowed {
		fmt.Fprintf(os.Stdout, "allowed\n")
		fmt.Fprintf(os.Stderr, "Access to %s: allowed (%s)\n", repoURL, result.Reason)
		return nil
	}
	fmt.Fprintf(os.Stdout, "denied\n")
	fmt.Fprintf(os.Stderr, "Access to %s: denied (%s)\n", repoURL, result.Reason)
	return fmt.Errorf("repo access denied: %s", result.Reason)
}
