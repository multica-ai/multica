// CEREBRO-PATCH(cerebro-mini-app-workflow-cli): FIR-3172 app workflow lifecycle.
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var appWorkflowCmd = &cobra.Command{Use: "workflow", Short: "Build and run app data workflows"}
var appWorkflowCreateCmd = &cobra.Command{Use: "create", Short: "Create an app workflow from JSON", Args: cobra.NoArgs, RunE: runAppWorkflowCreate}
var appWorkflowTestCmd = &cobra.Command{Use: "test <workflow-id>", Short: "Queue a test run", Args: exactArgs(1), RunE: runAppWorkflowTest}
var appWorkflowEnableCmd = &cobra.Command{Use: "enable <workflow-id>", Short: "Enable an app workflow", Args: exactArgs(1), RunE: runAppWorkflowState("enable")}
var appWorkflowDisableCmd = &cobra.Command{Use: "disable <workflow-id>", Short: "Disable an app workflow", Args: exactArgs(1), RunE: runAppWorkflowState("disable")}
var appWorkflowRunsCmd = &cobra.Command{Use: "runs <workflow-id>", Short: "List app workflow runs", Args: exactArgs(1), RunE: runAppWorkflowRuns}

func init() {
	appWorkflowCreateCmd.Flags().String("app", "", "App ID")
	appWorkflowCreateCmd.Flags().String("name", "", "Workflow name")
	appWorkflowCreateCmd.Flags().String("version", "1.0.0", "Workflow schema version")
	appWorkflowCreateCmd.Flags().String("file", "", "Workflow definition JSON file")
	appWorkflowTestCmd.Flags().String("file", "", "Optional trigger payload JSON file")
	appWorkflowCmd.AddCommand(appWorkflowCreateCmd, appWorkflowTestCmd, appWorkflowEnableCmd, appWorkflowDisableCmd, appWorkflowRunsCmd)
}

func runAppWorkflowCreate(cmd *cobra.Command, _ []string) error {
	appID, _ := cmd.Flags().GetString("app")
	name, _ := cmd.Flags().GetString("name")
	version, _ := cmd.Flags().GetString("version")
	file, _ := cmd.Flags().GetString("file")
	if appID == "" || name == "" || file == "" {
		return fmt.Errorf("--app, --name, and --file are required")
	}
	definition, err := readJSONFile(file)
	if err != nil {
		return err
	}
	return appPost(cmd, "/api/cerebro/app-workflows", map[string]any{"app_id": appID, "name": name, "version": version, "definition": definition})
}

func runAppWorkflowTest(cmd *cobra.Command, args []string) error {
	body := map[string]any{"trigger_payload": map[string]any{}}
	file, _ := cmd.Flags().GetString("file")
	if file != "" {
		payload, err := readJSONFile(file)
		if err != nil {
			return err
		}
		body["trigger_payload"] = payload
	}
	return appPost(cmd, "/api/cerebro/app-workflows/"+url.PathEscape(args[0])+"/test", body)
}

func runAppWorkflowState(state string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		return appPost(cmd, "/api/cerebro/app-workflows/"+url.PathEscape(args[0])+"/"+state, map[string]any{})
	}
}

func runAppWorkflowRuns(cmd *cobra.Command, args []string) error {
	client, ctx, cancel, err := appClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var output json.RawMessage
	if err := client.GetJSON(ctx, "/api/cerebro/app-workflows/"+url.PathEscape(args[0])+"/runs", &output); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, output)
}
