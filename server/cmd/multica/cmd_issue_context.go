package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var issueContextCmd = &cobra.Command{
	Use:   "context",
	Short: "Print the current daemon run context",
	RunE:  runIssueContext,
}

func init() {
	issueCmd.AddCommand(issueContextCmd)
	issueContextCmd.Flags().String("output", "json", "Output format: json")
}

func resolveRunContextPath() (string, error) {
	path := strings.TrimSpace(os.Getenv("MULTICA_RUN_CONTEXT"))
	if path != "" {
		return path, nil
	}
	if inAgentExecutionContext() {
		return "", fmt.Errorf("run context is unavailable: MULTICA_RUN_CONTEXT must be set by the daemon in agent execution context (no fallback to prose)")
	}
	return "", fmt.Errorf("run context is unavailable outside daemon execution: MULTICA_RUN_CONTEXT is not set")
}

func loadCurrentRunContext() (any, error) {
	path, err := resolveRunContextPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		taskID := strings.TrimSpace(os.Getenv("MULTICA_TASK_ID"))
		if taskID != "" {
			return nil, fmt.Errorf("run context file %q could not be read for task %s: %w (the run may be orphaned or the workdir was cleaned up)", path, taskID, err)
		}
		return nil, fmt.Errorf("read run context %q: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("run context file %q is empty", path)
	}

	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("run context file %q is invalid JSON: %w", path, err)
	}
	return payload, nil
}

func runIssueContext(cmd *cobra.Command, _ []string) error {
	output, _ := cmd.Flags().GetString("output")
	if output != "json" {
		return fmt.Errorf("--output must be json")
	}

	payload, err := loadCurrentRunContext()
	if err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, payload)
}
