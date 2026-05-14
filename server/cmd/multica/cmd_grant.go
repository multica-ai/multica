package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ---------------------------------------------------------------------------
// Grant commands — workspace-scoped CRUD for Persona grant control plane.
// multica grant list|get|create|update|delete
// ---------------------------------------------------------------------------

var grantCmd = &cobra.Command{
	Use:   "grant",
	Short: "Manage workspace grants (Persona permission policies)",
}

var grantListCmd = &cobra.Command{
	Use:   "list",
	Short: "List grants in the workspace",
	RunE:  runGrantList,
}

var grantGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get grant details",
	Args:  exactArgs(1),
	RunE:  runGrantGet,
}

var grantCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new grant",
	RunE:  runGrantCreate,
}

var grantUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a grant",
	Args:  exactArgs(1),
	RunE:  runGrantUpdate,
}

var grantDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a grant",
	Args:  exactArgs(1),
	RunE:  runGrantDelete,
}

func init() {
	grantCmd.AddCommand(grantListCmd)
	grantCmd.AddCommand(grantGetCmd)
	grantCmd.AddCommand(grantCreateCmd)
	grantCmd.AddCommand(grantUpdateCmd)
	grantCmd.AddCommand(grantDeleteCmd)

	// list
	grantListCmd.Flags().String("subject-type", "", "Filter by subject type (member, agent, group, role, workspace_default)")
	grantListCmd.Flags().String("subject-id", "", "Filter by subject UUID")
	grantListCmd.Flags().String("status", "", "Filter by status (active, revoked)")
	grantListCmd.Flags().String("output", "table", "Output format: table or json")
	grantListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	// get
	grantGetCmd.Flags().String("output", "json", "Output format: table or json")

	// create
	grantCreateCmd.Flags().String("subject-type", "", "Subject type: member, agent, group, role, workspace_default (required)")
	grantCreateCmd.Flags().String("subject-id", "", "Subject UUID (required unless --subject-type workspace_default)")
	grantCreateCmd.Flags().String("resource-pattern", "", "Resource pattern, e.g. 'issues/*' or '*' (required)")
	grantCreateCmd.Flags().String("capability", "", "Capability identifier, e.g. 'read' or 'issues:write' (required)")
	grantCreateCmd.Flags().String("classification-ceiling", "", "Maximum data-classification level allowed")
	grantCreateCmd.Flags().String("time-window-start", "", "Grant active from (RFC3339)")
	grantCreateCmd.Flags().String("time-window-end", "", "Grant active until (RFC3339)")
	grantCreateCmd.Flags().Bool("approval-required", false, "Require approval before the grant takes effect")
	grantCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// update
	grantUpdateCmd.Flags().String("resource-pattern", "", "New resource pattern")
	grantUpdateCmd.Flags().String("capability", "", "New capability identifier")
	grantUpdateCmd.Flags().String("classification-ceiling", "", "New classification ceiling")
	grantUpdateCmd.Flags().String("time-window-start", "", "New time-window start (RFC3339)")
	grantUpdateCmd.Flags().String("time-window-end", "", "New time-window end (RFC3339)")
	grantUpdateCmd.Flags().Bool("approval-required", false, "Set approval-required to true")
	grantUpdateCmd.Flags().Bool("no-approval-required", false, "Set approval-required to false")
	grantUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// delete
	grantDeleteCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func grantBasePath(client *cli.APIClient) string {
	if client.WorkspaceID == "" {
		return "/api/workspaces/unknown/grants"
	}
	return "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/grants"
}

func resolveGrantID(ctx context.Context, client *cli.APIClient, input string) (resolvedID, error) {
	return resolveIDByPrefix(ctx, client, "grant", input, fetchGrantCandidates)
}

func fetchGrantCandidates(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
	if client.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace_id is required to resolve grant id prefixes")
	}
	var result map[string]any
	if err := client.GetJSON(ctx, grantBasePath(client), &result); err != nil {
		return nil, err
	}
	grantsRaw, _ := result["grants"].([]any)
	candidates := make([]idCandidate, 0, len(grantsRaw))
	for _, raw := range grantsRaw {
		g, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		candidates = append(candidates, idCandidate{
			ID:      strVal(g, "id"),
			Display: strVal(g, "id"),
			Detail:  strVal(g, "subject_type") + ":" + strVal(g, "capability"),
		})
	}
	return candidates, nil
}

func grantRow(g map[string]any, fullID bool) []string {
	subjectID := strVal(g, "subject_id")
	if subjectID != "" && !fullID {
		subjectID = truncateID(subjectID)
	}
	granted := strVal(g, "granted_at")
	if len(granted) >= 10 {
		granted = granted[:10]
	}
	return []string{
		displayID(strVal(g, "id"), fullID),
		strVal(g, "subject_type"),
		subjectID,
		strVal(g, "resource_pattern"),
		strVal(g, "capability"),
		strVal(g, "status"),
		granted,
	}
}

// ---------------------------------------------------------------------------
// runGrantList
// ---------------------------------------------------------------------------

func runGrantList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := url.Values{}
	if v, _ := cmd.Flags().GetString("subject-type"); v != "" {
		params.Set("subject_type", v)
	}
	if v, _ := cmd.Flags().GetString("subject-id"); v != "" {
		params.Set("subject_id", v)
	}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		params.Set("status", v)
	}

	path := grantBasePath(client)
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list grants: %w", err)
	}
	grantsRaw, _ := result["grants"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, grantsRaw)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "SUBJECT_TYPE", "SUBJECT_ID", "RESOURCE_PATTERN", "CAPABILITY", "STATUS", "GRANTED"}
	rows := make([][]string, 0, len(grantsRaw))
	for _, raw := range grantsRaw {
		g, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, grantRow(g, fullID))
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// runGrantGet
// ---------------------------------------------------------------------------

func runGrantGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ref, err := resolveGrantID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve grant: %w", err)
	}

	var g map[string]any
	if err := client.GetJSON(ctx, grantBasePath(client)+"/"+url.PathEscape(ref.ID), &g); err != nil {
		return fmt.Errorf("get grant: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "SUBJECT_TYPE", "SUBJECT_ID", "RESOURCE_PATTERN", "CAPABILITY", "STATUS", "GRANTED"}
		cli.PrintTable(os.Stdout, headers, [][]string{grantRow(g, true)})
		return nil
	}
	return cli.PrintJSON(os.Stdout, g)
}

// ---------------------------------------------------------------------------
// runGrantCreate
// ---------------------------------------------------------------------------

func runGrantCreate(cmd *cobra.Command, _ []string) error {
	subjectType, _ := cmd.Flags().GetString("subject-type")
	resourcePattern, _ := cmd.Flags().GetString("resource-pattern")
	capability, _ := cmd.Flags().GetString("capability")
	if subjectType == "" {
		return fmt.Errorf("--subject-type is required")
	}
	if resourcePattern == "" {
		return fmt.Errorf("--resource-pattern is required")
	}
	if capability == "" {
		return fmt.Errorf("--capability is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{
		"subject_type":     subjectType,
		"resource_pattern": resourcePattern,
		"capability":       capability,
	}
	if v, _ := cmd.Flags().GetString("subject-id"); v != "" {
		body["subject_id"] = v
	}
	if v, _ := cmd.Flags().GetString("classification-ceiling"); v != "" {
		body["classification_ceiling"] = v
	}
	if v, _ := cmd.Flags().GetString("time-window-start"); v != "" {
		body["time_window_start"] = v
	}
	if v, _ := cmd.Flags().GetString("time-window-end"); v != "" {
		body["time_window_end"] = v
	}
	if v, _ := cmd.Flags().GetBool("approval-required"); v {
		body["approval_required"] = true
	}

	var result map[string]any
	if err := client.PostJSON(ctx, grantBasePath(client), body, &result); err != nil {
		return fmt.Errorf("create grant: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "SUBJECT_TYPE", "RESOURCE_PATTERN", "CAPABILITY", "STATUS"}
		cli.PrintTable(os.Stdout, headers, [][]string{{
			strVal(result, "id"),
			strVal(result, "subject_type"),
			strVal(result, "resource_pattern"),
			strVal(result, "capability"),
			strVal(result, "status"),
		}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---------------------------------------------------------------------------
// runGrantUpdate
// ---------------------------------------------------------------------------

func runGrantUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ref, err := resolveGrantID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve grant: %w", err)
	}

	body := map[string]any{}
	if v, _ := cmd.Flags().GetString("resource-pattern"); v != "" {
		body["resource_pattern"] = v
	}
	if v, _ := cmd.Flags().GetString("capability"); v != "" {
		body["capability"] = v
	}
	if v, _ := cmd.Flags().GetString("classification-ceiling"); v != "" {
		body["classification_ceiling"] = v
	}
	if v, _ := cmd.Flags().GetString("time-window-start"); v != "" {
		body["time_window_start"] = v
	}
	if v, _ := cmd.Flags().GetString("time-window-end"); v != "" {
		body["time_window_end"] = v
	}
	noApproval, _ := cmd.Flags().GetBool("no-approval-required")
	approval, _ := cmd.Flags().GetBool("approval-required")
	if noApproval {
		body["approval_required"] = false
	} else if approval {
		body["approval_required"] = true
	}

	if len(body) == 0 {
		return fmt.Errorf("nothing to update — provide at least one flag")
	}

	var result map[string]any
	if err := client.PatchJSON(ctx, grantBasePath(client)+"/"+url.PathEscape(ref.ID), body, &result); err != nil {
		return fmt.Errorf("update grant: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "SUBJECT_TYPE", "RESOURCE_PATTERN", "CAPABILITY", "STATUS"}
		cli.PrintTable(os.Stdout, headers, [][]string{{
			strVal(result, "id"),
			strVal(result, "subject_type"),
			strVal(result, "resource_pattern"),
			strVal(result, "capability"),
			strVal(result, "status"),
		}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---------------------------------------------------------------------------
// runGrantDelete
// ---------------------------------------------------------------------------

func runGrantDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ref, err := resolveGrantID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve grant: %w", err)
	}

	if err := client.DeleteJSON(ctx, grantBasePath(client)+"/"+url.PathEscape(ref.ID)); err != nil {
		return fmt.Errorf("delete grant: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"id": ref.ID, "deleted": true})
	}
	fmt.Fprintf(os.Stdout, "Grant %s deleted.\n", ref.Display)
	return nil
}
