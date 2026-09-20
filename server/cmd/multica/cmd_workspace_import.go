package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var workspaceImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Copy agents and squads from another workspace you belong to",
	Long: `Copy portable agent and squad configuration from another workspace
into the current workspace.

The source workspace is left untouched. Secrets and machine-local fields are
never copied: custom_env, mcp_config, and runtime_config. Skills attach only
when a skill of the same name already exists in the current workspace.

Imported agents are bound to --runtime-id in the current workspace. Squads
remap their agent members onto the newly created (or reused) agents. Human
squad members are copied only when they also belong to the current workspace.

By default, names that already exist here are skipped and reused for squad
remapping. Pass --on-conflict rename to create copies named "<name> (imported)".

Pass --preview to list what would be imported without writing anything.`,
	Example: "  multica workspace import --from other-team --runtime-id <runtime-id>\n" +
		"  multica workspace import --from other-team --preview",
	Args: cobra.NoArgs,
	RunE: runWorkspaceImport,
}

func init() {
	workspaceCmd.AddCommand(workspaceImportCmd)
	workspaceImportCmd.Flags().String("from", "", "Source workspace id, slug, or id prefix")
	workspaceImportCmd.Flags().String("runtime-id", "", "Runtime in the current workspace for imported agents")
	workspaceImportCmd.Flags().StringSlice("agent-id", nil, "Import only these agent ids. Repeatable. Selecting a squad still pulls its agent members.")
	workspaceImportCmd.Flags().StringSlice("squad-id", nil, "Import only these squad ids. Repeatable.")
	workspaceImportCmd.Flags().Bool("all", false, "Import every agent and squad you can see in the source workspace")
	workspaceImportCmd.Flags().String("on-conflict", "skip", "When a name already exists here: skip or rename")
	workspaceImportCmd.Flags().Bool("preview", false, "List importable agents and squads without writing")
	workspaceImportCmd.Flags().String("output", "json", "Output format: table or json")
}

func runWorkspaceImport(cmd *cobra.Command, args []string) error {
	from, _ := cmd.Flags().GetString("from")
	if from == "" {
		return fmt.Errorf("--from is required")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	source, err := resolveWorkspaceRef(ctx, cmd, from)
	if err != nil {
		return err
	}
	targetID := resolveWorkspaceID(cmd)
	if targetID == "" {
		return fmt.Errorf("current workspace is required: set --workspace-id or run 'multica workspace switch'")
	}
	if source.ID == targetID {
		return fmt.Errorf("source workspace must be different from the current workspace")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	previewOnly, _ := cmd.Flags().GetBool("preview")
	if previewOnly {
		var preview map[string]any
		path := "/api/workspaces/" + targetID + "/import-preview?source_workspace_id=" + source.ID
		if err := client.GetJSON(ctx, path, &preview); err != nil {
			return fmt.Errorf("preview import: %w", err)
		}
		return printWorkspaceImportPreview(cmd, preview)
	}

	runtimeID, _ := cmd.Flags().GetString("runtime-id")
	if runtimeID == "" {
		return fmt.Errorf("--runtime-id is required (the runtime imported agents will use in this workspace)")
	}
	onConflict, _ := cmd.Flags().GetString("on-conflict")
	agentIDs, _ := cmd.Flags().GetStringSlice("agent-id")
	squadIDs, _ := cmd.Flags().GetStringSlice("squad-id")
	importAll, _ := cmd.Flags().GetBool("all")
	if !importAll && len(agentIDs) == 0 && len(squadIDs) == 0 {
		importAll = true
	}

	body := map[string]any{
		"source_workspace_id": source.ID,
		"runtime_id":          runtimeID,
		"import_all":          importAll,
		"on_conflict":         onConflict,
	}
	if !importAll {
		body["agent_ids"] = agentIDs
		body["squad_ids"] = squadIDs
	}

	var result map[string]any
	path := "/api/workspaces/" + targetID + "/import-from-workspace"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("import from workspace: %w", err)
	}
	return printWorkspaceImportResult(cmd, result)
}

func printWorkspaceImportPreview(cmd *cobra.Command, preview map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, preview)
	}
	fmt.Fprintf(os.Stdout, "Source: %s\n", strVal(preview, "source_workspace_name"))
	agents, _ := preview["agents"].([]any)
	squads, _ := preview["squads"].([]any)
	fmt.Fprintf(os.Stdout, "Agents (%d):\n", len(agents))
	for _, raw := range agents {
		item, _ := raw.(map[string]any)
		fmt.Fprintf(os.Stdout, "  %s  %s\n", strVal(item, "id"), strVal(item, "name"))
	}
	fmt.Fprintf(os.Stdout, "Squads (%d):\n", len(squads))
	for _, raw := range squads {
		item, _ := raw.(map[string]any)
		fmt.Fprintf(os.Stdout, "  %s  %s\n", strVal(item, "id"), strVal(item, "name"))
	}
	return nil
}

func printWorkspaceImportResult(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	printImportItems("Agents", result["agents"])
	printImportItems("Squads", result["squads"])
	return nil
}

func printImportItems(label string, raw any) {
	items, _ := raw.([]any)
	fmt.Fprintf(os.Stdout, "%s (%d):\n", label, len(items))
	for _, itemRaw := range items {
		item, _ := itemRaw.(map[string]any)
		fmt.Fprintf(os.Stdout, "  %s  %s  %s\n", strVal(item, "status"), strVal(item, "name"), strVal(item, "id"))
	}
}
