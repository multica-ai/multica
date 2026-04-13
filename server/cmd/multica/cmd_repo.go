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
	Use:   "checkout <url-or-path>",
	Short: "Check out a repository into the working directory",
	Long: `Creates a git worktree for this task and prints its path on stdout.

For GitHub workspace repos, the source is the daemon's bare clone cache.
For local workspace repos (type=local), the source is the user's on-disk
.git dir — remotes and history are shared, but the user's working tree is
never touched.

The first positional argument can be a GitHub URL or an absolute local path.
The --type flag forces interpretation; otherwise the daemon auto-detects.`,
	Args: exactArgs(1),
	RunE: runRepoCheckout,
}

func init() {
	repoCmd.AddCommand(repoCheckoutCmd)
	repoCheckoutCmd.Flags().String("type", "", "Force repo type: 'github' or 'local' (default: auto-detect)")
	repoCheckoutCmd.Flags().String("local-path", "", "Explicit local path (alternative to passing the path as the positional arg)")
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

	repoType, _ := cmd.Flags().GetString("type")
	localPath, _ := cmd.Flags().GetString("local-path")

	reqBody := map[string]string{
		"url":          repoURL,
		"workspace_id": workspaceID,
		"workdir":      workDir,
		"agent_name":   agentName,
		"task_id":      taskID,
	}
	if repoType != "" {
		reqBody["type"] = repoType
	}
	if localPath != "" {
		reqBody["local_path"] = localPath
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
