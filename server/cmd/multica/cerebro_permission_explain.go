package main

// cerebro_permission_explain.go — `multica permission explain` (FIR-1496).
//
// Given ONE subject (a workspace member or an agent) it reports, for every
// platform/tool capability, the EFFECTIVE permission and WHY: which layer
// decided it, plus how_to_fix and who_can_fix. It is a read-only view over the
// new GET /api/workspaces/{id}/permissions/explain endpoint and mirrors the
// grant CLI (cobra + newAPIClient + client.GetJSON + cli.PrintJSON/PrintTable).

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var permissionCmd = &cobra.Command{
	Use:   "permission",
	Short: "Inspect a subject's effective permissions and why",
}

var permissionExplainCmd = &cobra.Command{
	Use:   "explain",
	Short: "Explain a subject's effective permission for every capability",
	RunE:  runPermissionExplain,
}

func init() {
	permissionCmd.AddCommand(permissionExplainCmd)

	permissionExplainCmd.Flags().String("subject-type", "", "Subject type: member or agent (required)")
	permissionExplainCmd.Flags().String("subject-id", "", "Subject UUID (required)")
	permissionExplainCmd.Flags().String("capability", "", "Filter to one capability (tool_key)")
	permissionExplainCmd.Flags().String("output", "table", "Output format: table or json")
}

func permissionExplainBasePath(client *cli.APIClient) string {
	if client.WorkspaceID == "" {
		return "/api/workspaces/unknown/permissions/explain"
	}
	return "/api/workspaces/" + url.PathEscape(client.WorkspaceID) + "/permissions/explain"
}

func runPermissionExplain(cmd *cobra.Command, _ []string) error {
	subjectType, _ := cmd.Flags().GetString("subject-type")
	subjectID, _ := cmd.Flags().GetString("subject-id")
	if subjectType != "member" && subjectType != "agent" {
		return fmt.Errorf("--subject-type must be 'member' or 'agent'")
	}
	if subjectID == "" {
		return fmt.Errorf("--subject-id is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("subject_type", subjectType)
	params.Set("subject_id", subjectID)
	if v, _ := cmd.Flags().GetString("capability"); v != "" {
		params.Set("capability", v)
	}

	path := permissionExplainBasePath(client) + "?" + params.Encode()

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("explain permissions: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	capsRaw, _ := result["capabilities"].([]any)
	headers := []string{"CAPABILITY", "EFFECTIVE", "DECIDED_BY", "WHY", "HOW_TO_FIX"}
	rows := make([][]string, 0, len(capsRaw))
	for _, raw := range capsRaw {
		c, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		eff, _ := c["effective"].(map[string]any)
		rows = append(rows, []string{
			strVal(c, "tool_key"),
			strVal(eff, "setting"),
			strVal(eff, "decided_by"),
			truncateText(strVal(eff, "reason"), 50),
			truncateText(strVal(c, "how_to_fix"), 50),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func truncateText(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 1 {
		return s[:max]
	}
	return s[:max-1] + "…"
}
