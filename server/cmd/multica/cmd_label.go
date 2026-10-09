package main

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ---------------------------------------------------------------------------
// Label commands — workspace-scoped CRUD for labels (issues and skills).
// ---------------------------------------------------------------------------

var labelCmd = &cobra.Command{
	Use:   "label",
	Short: "Work with labels",
}

var labelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List labels in the workspace",
	RunE:  runLabelList,
}

var labelGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get label details",
	Args:  exactArgs(1),
	RunE:  runLabelGet,
}

var labelCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new label",
	RunE:  runLabelCreate,
}

var labelUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a label",
	Args:  exactArgs(1),
	RunE:  runLabelUpdate,
}

var labelDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a label",
	Args:  exactArgs(1),
	RunE:  runLabelDelete,
}

func validateLabelResourceType(value string) error {
	if value != "issue" && value != "skill" {
		return fmt.Errorf("resource type must be issue or skill")
	}
	return nil
}

func init() {
	labelCmd.AddCommand(labelListCmd)
	labelCmd.AddCommand(labelGetCmd)
	labelCmd.AddCommand(labelCreateCmd)
	labelCmd.AddCommand(labelUpdateCmd)
	labelCmd.AddCommand(labelDeleteCmd)

	labelListCmd.Flags().String("output", "table", "Output format: table or json")
	labelListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	labelListCmd.Flags().String("resource-type", "", "Filter by resource type: issue or skill (default: issue)")
	labelGetCmd.Flags().String("output", "json", "Output format: table or json")

	labelCreateCmd.Flags().String("name", "", "Label name (required)")
	labelCreateCmd.Flags().String("color", "", "Hex color like #3b82f6 (required)")
	labelCreateCmd.Flags().String("resource-type", "issue", "Resource type: issue or skill")
	labelCreateCmd.Flags().String("description", "", "Label description")
	labelCreateCmd.Flags().String("output", "json", "Output format: table or json")

	labelUpdateCmd.Flags().String("name", "", "New name")
	labelUpdateCmd.Flags().String("color", "", "New hex color")
	labelUpdateCmd.Flags().String("description", "", "New description")
	labelUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	labelDeleteCmd.Flags().String("output", "json", "Output format: table or json")
}

func runLabelList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	if v, _ := cmd.Flags().GetString("resource-type"); v != "" {
		if err := validateLabelResourceType(v); err != nil {
			return err
		}
		params.Set("resource_type", v)
	}
	path := "/api/labels"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list labels: %w", err)
	}
	labelsRaw, _ := result["labels"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, labelsRaw)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "NAME", "RESOURCE TYPE", "COLOR", "CREATED"}
	rows := make([][]string, 0, len(labelsRaw))
	for _, raw := range labelsRaw {
		l, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		created := strVal(l, "created_at")
		if len(created) >= 10 {
			created = created[:10]
		}
		rows = append(rows, []string{
			displayID(strVal(l, "id"), fullID),
			strVal(l, "name"),
			strVal(l, "resource_type"),
			strVal(l, "color"),
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runLabelGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	labelRef, err := resolveLabelID(ctx, client, args[0], "")
	if err != nil {
		return fmt.Errorf("resolve label: %w", err)
	}

	var label map[string]any
	if err := client.GetJSON(ctx, "/api/labels/"+labelRef.ID, &label); err != nil {
		return fmt.Errorf("get label: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "RESOURCE TYPE", "COLOR", "CREATED"}
		created := strVal(label, "created_at")
		if len(created) >= 10 {
			created = created[:10]
		}
		rows := [][]string{{
			strVal(label, "id"),
			strVal(label, "name"),
			strVal(label, "resource_type"),
			strVal(label, "color"),
			created,
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, label)
}

func runLabelCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	color, _ := cmd.Flags().GetString("color")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if color == "" {
		return fmt.Errorf("--color is required (e.g. #3b82f6)")
	}
	resourceType, _ := cmd.Flags().GetString("resource-type")
	if err := validateLabelResourceType(resourceType); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"name": name, "color": color}
	body["resource_type"] = resourceType
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/labels", body, &result); err != nil {
		return fmt.Errorf("create label: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "RESOURCE TYPE", "COLOR"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "name"),
			strVal(result, "resource_type"),
			strVal(result, "color"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runLabelUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	labelRef, err := resolveLabelID(ctx, client, args[0], "")
	if err != nil {
		return fmt.Errorf("resolve label: %w", err)
	}

	body := map[string]any{}
	if v, _ := cmd.Flags().GetString("name"); v != "" {
		body["name"] = v
	}
	if v, _ := cmd.Flags().GetString("color"); v != "" {
		body["color"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to update — provide --name, --color, and/or --description")
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/labels/"+labelRef.ID, body, &result); err != nil {
		return fmt.Errorf("update label: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "NAME", "RESOURCE TYPE", "COLOR"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "name"),
			strVal(result, "resource_type"),
			strVal(result, "color"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runLabelDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	labelRef, err := resolveLabelID(ctx, client, args[0], "")
	if err != nil {
		return fmt.Errorf("resolve label: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/labels/"+labelRef.ID); err != nil {
		return fmt.Errorf("delete label: %w", err)
	}
	// JSON consumers get machine-readable output; humans get natural language.
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"id": labelRef.ID, "deleted": true})
	}
	fmt.Fprintf(os.Stdout, "Label %s deleted.\n", labelRef.Display)
	return nil
}
