// CEREBRO-PATCH(cerebro-workflow-hooks-cli): FIR-3101 agent hook management surface.
package main

import (
	"context"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var workflowHookCmd = &cobra.Command{Use: "hook", Short: "Manage Workflow hooks"}
var workflowHookListCmd = &cobra.Command{Use: "list", Short: "List Workflow hooks", Args: cobra.NoArgs, RunE: runWorkflowHookList}
var workflowHookGetCmd = &cobra.Command{Use: "get <hook-id>", Short: "Get a Workflow hook", Args: exactArgs(1), RunE: runWorkflowHookGet}
var workflowHookCreateCmd = &cobra.Command{Use: "create", Short: "Create a Workflow hook in Dry run", Args: cobra.NoArgs, RunE: runWorkflowHookCreate}
var workflowHookUpdateCmd = &cobra.Command{Use: "update <hook-id>", Short: "Update a Workflow hook as a new Dry run version", Args: exactArgs(1), RunE: runWorkflowHookUpdate}
var workflowHookTestCmd = &cobra.Command{Use: "test <hook-id>", Short: "Test a Workflow hook without side effects", Args: exactArgs(1), RunE: runWorkflowHookTest}
var workflowHookPublishCmd = &cobra.Command{Use: "publish <hook-id>", Short: "Publish a tested Workflow hook", Args: exactArgs(1), RunE: runWorkflowHookPublish}
var workflowHookEffectiveCmd = &cobra.Command{Use: "effective <hook-id>", Short: "Show effective Workflow hook bindings", Args: exactArgs(1), RunE: runWorkflowHookEffective}
var workflowHookRunsCmd = &cobra.Command{Use: "runs <hook-id>", Short: "List Workflow hook runs", Args: exactArgs(1), RunE: runWorkflowHookRuns}

func init() {
	for _, cmd := range []*cobra.Command{workflowHookListCmd, workflowHookGetCmd, workflowHookCreateCmd, workflowHookUpdateCmd, workflowHookTestCmd, workflowHookPublishCmd, workflowHookEffectiveCmd, workflowHookRunsCmd} {
		cmd.Flags().String("output", "json", "Output format: json")
	}
	for _, cmd := range []*cobra.Command{workflowHookCreateCmd, workflowHookUpdateCmd, workflowHookTestCmd} {
		cmd.Flags().String("file", "", "Read the JSON document from a file")
		cmd.Flags().Bool("stdin", false, "Read the JSON document from stdin")
	}
	workflowHookCmd.AddCommand(workflowHookListCmd, workflowHookGetCmd, workflowHookCreateCmd, workflowHookUpdateCmd, workflowHookTestCmd, workflowHookPublishCmd, workflowHookEffectiveCmd, workflowHookRunsCmd)
	workflowCmd.AddCommand(workflowHookCmd)
}

func workflowHookClient(cmd *cobra.Command) (*cli.APIClient, context.Context, context.CancelFunc, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	return client, ctx, cancel, nil
}

func runWorkflowHookList(cmd *cobra.Command, _ []string) error {
	return workflowHookGetPath(cmd, "/api/cerebro/workflow-hooks")
}

func runWorkflowHookGet(cmd *cobra.Command, args []string) error {
	return workflowHookGetPath(cmd, "/api/cerebro/workflow-hooks/"+url.PathEscape(args[0]))
}

func runWorkflowHookEffective(cmd *cobra.Command, args []string) error {
	return workflowHookGetPath(cmd, "/api/cerebro/workflow-hooks/"+url.PathEscape(args[0])+"/effective")
}

func runWorkflowHookRuns(cmd *cobra.Command, args []string) error {
	return workflowHookGetPath(cmd, "/api/cerebro/workflow-hooks/"+url.PathEscape(args[0])+"/runs")
}

func workflowHookGetPath(cmd *cobra.Command, path string) error {
	client, ctx, cancel, err := workflowHookClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out any
	if err := client.GetJSON(ctx, path, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runWorkflowHookCreate(cmd *cobra.Command, _ []string) error {
	body, err := readWorkflowBody(cmd)
	if err != nil {
		return err
	}
	return workflowHookPost(cmd, "/api/cerebro/workflow-hooks", body)
}

func runWorkflowHookUpdate(cmd *cobra.Command, args []string) error {
	body, err := readWorkflowBody(cmd)
	if err != nil {
		return err
	}
	client, ctx, cancel, err := workflowHookClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out any
	if err := client.PutJSON(ctx, "/api/cerebro/workflow-hooks/"+url.PathEscape(args[0]), body, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}

func runWorkflowHookTest(cmd *cobra.Command, args []string) error {
	body := map[string]any{}
	if file, _ := cmd.Flags().GetString("file"); file != "" {
		var err error
		body, err = readWorkflowBody(cmd)
		if err != nil {
			return err
		}
	} else if stdin, _ := cmd.Flags().GetBool("stdin"); stdin {
		var err error
		body, err = readWorkflowBody(cmd)
		if err != nil {
			return err
		}
	}
	return workflowHookPost(cmd, "/api/cerebro/workflow-hooks/"+url.PathEscape(args[0])+"/test", body)
}

func runWorkflowHookPublish(cmd *cobra.Command, args []string) error {
	return workflowHookPost(cmd, "/api/cerebro/workflow-hooks/"+url.PathEscape(args[0])+"/publish", map[string]any{})
}

func workflowHookPost(cmd *cobra.Command, path string, body map[string]any) error {
	client, ctx, cancel, err := workflowHookClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out any
	if err := client.PostJSON(ctx, path, body, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}
