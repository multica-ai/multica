// CEREBRO-PATCH(cerebro-workflow-cli): FIR-2283 followup — a minimal `multica
// workflow` command so agents can discover Issue workflow recipe IDs from the
// CLI. Without it, `multica issue create --workflow <id>` has no CLI-native way
// to find the <id>; the recipe list was only reachable from the web UI.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
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

// CEREBRO-PATCH(cerebro-workflow-cli-management): FIR-2937 agent workflow management commands.
var workflowGetCmd = &cobra.Command{Use: "get <workflow-id>", Short: "Get a workflow", Args: exactArgs(1), RunE: runWorkflowGet}
var workflowCreateCmd = &cobra.Command{Use: "create", Short: "Create a workflow from a JSON document", Args: cobra.NoArgs, RunE: runWorkflowCreate}
var workflowUpdateCmd = &cobra.Command{Use: "update <workflow-id>", Short: "Replace a workflow from a JSON document", Args: exactArgs(1), RunE: runWorkflowUpdate}
var workflowDeleteCmd = &cobra.Command{Use: "delete <workflow-id>", Short: "Delete a workflow", Args: exactArgs(1), RunE: runWorkflowDelete}
var workflowToggleCmd = &cobra.Command{Use: "toggle <workflow-id>", Short: "Enable or disable a workflow", Args: exactArgs(1), RunE: runWorkflowToggle}
var workflowActivateCmd = &cobra.Command{Use: "activate <workflow-id> <issue-id>", Short: "Activate an Issue workflow on an existing issue", Args: exactArgs(2), RunE: runWorkflowActivate}
var workflowForIssueCmd = &cobra.Command{Use: "for-issue <issue-id>", Short: "Get the active workflow for an issue", Args: exactArgs(1), RunE: runWorkflowForIssue}

func init() {
	workflowListCmd.Flags().Bool("all", false, "Include standard workflows, not just Issue workflow recipes")
	workflowListCmd.Flags().String("output", "table", "Output format: table or json")
	for _, cmd := range []*cobra.Command{workflowGetCmd, workflowCreateCmd, workflowUpdateCmd, workflowDeleteCmd, workflowToggleCmd, workflowActivateCmd, workflowForIssueCmd} {
		cmd.Flags().String("output", "json", "Output format: json")
	}
	for _, cmd := range []*cobra.Command{workflowCreateCmd, workflowUpdateCmd} {
		cmd.Flags().String("file", "", "Read the complete workflow JSON document from a file")
		cmd.Flags().Bool("stdin", false, "Read the complete workflow JSON document from stdin")
	}
	workflowToggleCmd.Flags().Bool("enabled", true, "Whether the workflow should be enabled")
	workflowCmd.AddCommand(workflowListCmd, workflowGetCmd, workflowCreateCmd, workflowUpdateCmd, workflowDeleteCmd, workflowToggleCmd, workflowActivateCmd, workflowForIssueCmd)
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

func workflowClient(cmd *cobra.Command) (*cli.APIClient, context.Context, context.CancelFunc, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	return client, ctx, cancel, nil
}

func runWorkflowGet(cmd *cobra.Command, args []string) error {
	client, ctx, cancel, err := workflowClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out any
	if err := client.GetJSON(ctx, "/api/cerebro/workflows/"+url.PathEscape(args[0]), &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func readWorkflowBody(cmd *cobra.Command) (map[string]any, error) {
	file, _ := cmd.Flags().GetString("file")
	stdin, _ := cmd.Flags().GetBool("stdin")
	if file != "" && stdin {
		return nil, fmt.Errorf("--file and --stdin are mutually exclusive")
	}
	if file == "" && !stdin {
		return nil, fmt.Errorf("one of --file or --stdin is required")
	}
	var raw []byte
	var err error
	if file != "" {
		raw, err = os.ReadFile(file)
	} else {
		raw, err = io.ReadAll(cmd.InOrStdin())
	}
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("invalid workflow JSON: %w", err)
	}
	if body == nil {
		return nil, fmt.Errorf("workflow JSON must be an object")
	}
	return body, nil
}

func runWorkflowCreate(cmd *cobra.Command, _ []string) error {
	body, err := readWorkflowBody(cmd)
	if err != nil {
		return err
	}
	client, ctx, cancel, err := workflowClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out any
	if err := client.PostJSON(ctx, "/api/cerebro/workflows", body, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runWorkflowUpdate(cmd *cobra.Command, args []string) error {
	body, err := readWorkflowBody(cmd)
	if err != nil {
		return err
	}
	client, ctx, cancel, err := workflowClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out any
	if err := client.PutJSON(ctx, "/api/cerebro/workflows/"+url.PathEscape(args[0]), body, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runWorkflowDelete(cmd *cobra.Command, args []string) error {
	client, ctx, cancel, err := workflowClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	if err := client.DeleteJSON(ctx, "/api/cerebro/workflows/"+url.PathEscape(args[0])); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, map[string]any{"deleted": args[0]})
}

func runWorkflowToggle(cmd *cobra.Command, args []string) error {
	enabled, _ := cmd.Flags().GetBool("enabled")
	client, ctx, cancel, err := workflowClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out any
	if err := client.PostJSON(ctx, "/api/cerebro/workflows/"+url.PathEscape(args[0])+"/toggle", map[string]any{"enabled": enabled}, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runWorkflowActivate(cmd *cobra.Command, args []string) error {
	client, ctx, cancel, err := workflowClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out any
	if err := client.PostJSON(ctx, "/api/cerebro/workflows/"+url.PathEscape(args[0])+"/activate", map[string]any{"issue_id": args[1]}, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runWorkflowForIssue(cmd *cobra.Command, args []string) error {
	client, ctx, cancel, err := workflowClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out any
	if err := client.GetJSON(ctx, "/api/cerebro/workflows/for-issue/"+url.PathEscape(args[0]), &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}
