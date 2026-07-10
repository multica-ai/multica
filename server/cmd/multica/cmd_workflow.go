// CEREBRO-PATCH(cerebro-workflow-cli): FIR-2283 followup — a minimal `multica
// workflow` command so agents can discover Issue workflow recipe IDs from the
// CLI. Without it, `multica issue create --workflow <id>` has no CLI-native way
// to find the <id>; the recipe list was only reachable from the web UI.
package main

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Inspect workflows (including Issue workflow recipes)",
	Long: "Work with workflows in the current workspace.\n\n" +
		"An Issue workflow recipe is what `multica issue create --workflow <id>` " +
		"starts a new issue on. Use `multica workflow list` to find its ID.",
}

var workflowListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workflows in the workspace",
	Long: "List workflows in the current workspace.\n\n" +
		"By default only Issue workflow recipes are shown — those are the ones an " +
		"issue can be started on (via the create modal's picker or " +
		"`multica issue create --workflow <id>`). Pass --all to include standard " +
		"workflows too.",
	RunE: runWorkflowList,
}

func init() {
	workflowListCmd.Flags().Bool("all", false, "Include standard workflows, not just Issue workflow recipes")
	workflowListCmd.Flags().String("output", "table", "Output format: table or json")
	workflowCmd.AddCommand(workflowListCmd)
}

func runWorkflowList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var resp struct {
		Workflows []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Enabled      bool   `json:"enabled"`
			WorkflowType string `json:"workflow_type"`
		} `json:"workflows"`
	}
	if err := client.GetJSON(ctx, "/api/cerebro/workflows", &resp); err != nil {
		return err
	}

	all, _ := cmd.Flags().GetBool("all")
	filtered := resp.Workflows[:0]
	for _, wf := range resp.Workflows {
		if all || wf.WorkflowType == "issue_loop" {
			filtered = append(filtered, wf)
		}
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"workflows": filtered})
	}

	headers := []string{"ID", "NAME", "TYPE", "ENABLED"}
	rows := make([][]string, 0, len(filtered))
	for _, wf := range filtered {
		wType := wf.WorkflowType
		if wType == "issue_loop" {
			wType = "Issue workflow"
		}
		enabled := "no"
		if wf.Enabled {
			enabled = "yes"
		}
		rows = append(rows, []string{wf.ID, wf.Name, wType, enabled})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
