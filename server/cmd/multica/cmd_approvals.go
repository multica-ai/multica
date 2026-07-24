package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var approvalsCmd = &cobra.Command{
	Use:   "approvals",
	Short: "Work with workspace approval and information requests",
}

var approvalsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workspace follow-ups that need approval or information",
	RunE:  runApprovalsList,
}

func init() {
	approvalsCmd.AddCommand(approvalsListCmd)

	approvalsListCmd.Flags().String("kind", "all", "Filter kind: needs-approval, needs-info, or all")
	approvalsListCmd.Flags().String("assignee", "", "Filter by assignee name")
	approvalsListCmd.Flags().String("assignee-id", "", "Filter by assignee UUID")
	approvalsListCmd.Flags().String("project", "", "Filter by project ID")
	approvalsListCmd.Flags().String("since", "", "Only show items updated after this RFC3339 timestamp")
	approvalsListCmd.Flags().Int("limit", 50, "Maximum number of items to return")
	approvalsListCmd.Flags().String("output", "table", "Output format: table or json")
	approvalsListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
}

type approvalsListResult struct {
	WorkspaceID string           `json:"workspace_id"`
	Items       []map[string]any `json:"items"`
	Counts      map[string]int   `json:"counts"`
}

func runApprovalsList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	kind, _ := cmd.Flags().GetString("kind")
	if kind != "all" && kind != followupDispositionNeedsApproval && kind != followupDispositionNeedsInfo {
		return fmt.Errorf("--kind must be needs-approval, needs-info, or all")
	}
	limit, _ := cmd.Flags().GetInt("limit")
	items, err := listApprovalInboxItems(ctx, client, cmd, kind, limit)
	if err != nil {
		return err
	}
	result := approvalsListResult{
		WorkspaceID: client.WorkspaceID,
		Items:       items,
		Counts:      approvalCounts(items),
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printApprovalsTable(result, fullID)
	return nil
}

func listApprovalInboxItems(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, kind string, limit int) ([]map[string]any, error) {
	dispositions := []string{followupDispositionNeedsApproval, followupDispositionNeedsInfo}
	if kind != "all" {
		dispositions = []string{kind}
	}
	items := []map[string]any{}
	for _, disposition := range dispositions {
		issues, err := queryIssuesByDisposition(ctx, client, cmd, disposition, limit)
		if err != nil {
			return nil, err
		}
		items = append(items, issues...)
	}
	since, _ := cmd.Flags().GetString("since")
	filtered := items[:0]
	for _, issue := range items {
		status := strVal(issue, "status")
		if status == "done" || status == "cancelled" {
			continue
		}
		if since != "" && strVal(issue, "updated_at") < since {
			continue
		}
		filtered = append(filtered, issue)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return approvalSortKey(filtered[i]) < approvalSortKey(filtered[j])
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, nil
}

func queryIssuesByDisposition(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, disposition string, limit int) ([]map[string]any, error) {
	params := url.Values{}
	params.Set("workspace_id", client.WorkspaceID)
	params.Set("limit", fmt.Sprintf("%d", limit))
	filter, err := buildMetadataFilterQueryParam([]string{"followup_disposition=" + disposition})
	if err != nil {
		return nil, err
	}
	params.Set("metadata", filter)
	if project, _ := cmd.Flags().GetString("project"); project != "" {
		projectRef, err := resolveProjectID(ctx, client, project)
		if err != nil {
			return nil, fmt.Errorf("resolve project: %w", err)
		}
		params.Set("project_id", projectRef.ID)
	}
	_, assigneeID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id", issueAssigneeKinds)
	if resolveErr != nil {
		return nil, fmt.Errorf("resolve assignee: %w", resolveErr)
	}
	if hasAssignee {
		params.Set("assignee_id", assigneeID)
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/issues?"+params.Encode(), &result); err != nil {
		return nil, fmt.Errorf("list approvals: %w", err)
	}
	raw, _ := result["issues"].([]any)
	issues := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if issue, ok := item.(map[string]any); ok {
			issues = append(issues, issue)
		}
	}
	return issues, nil
}

func approvalCounts(items []map[string]any) map[string]int {
	counts := map[string]int{followupDispositionNeedsApproval: 0, followupDispositionNeedsInfo: 0}
	for _, issue := range items {
		disposition := stringFromAny(metadataMap(issue)["followup_disposition"])
		counts[disposition]++
	}
	return counts
}

func approvalSortKey(issue map[string]any) string {
	metadata := metadataMap(issue)
	disposition := stringFromAny(metadata["followup_disposition"])
	dispositionRank := 1
	if disposition == followupDispositionNeedsApproval {
		dispositionRank = 0
	}
	riskRank := map[string]int{"high": 0, "medium": 1, "low": 2}[stringFromAny(metadata["risk_level"])]
	return fmt.Sprintf("%d:%d:%s", dispositionRank, riskRank, strVal(issue, "updated_at"))
}

func printApprovalsTable(result approvalsListResult, fullID bool) {
	fmt.Fprintf(os.Stdout, "Approvals Inbox (workspace: %s)\n\n", result.WorkspaceID)
	headers := []string{"#", "KIND", "KEY", "RISK", "ASK", "PARENT"}
	if fullID {
		headers = []string{"#", "KIND", "ID", "KEY", "RISK", "ASK", "PARENT"}
	}
	rows := make([][]string, 0, len(result.Items))
	for i, issue := range result.Items {
		metadata := metadataMap(issue)
		ask := stringFromAny(metadata["approval_ask"])
		if ask == "" {
			ask = stringFromAny(metadata["info_ask"])
		}
		row := []string{
			fmt.Sprintf("%d", i+1),
			stringFromAny(metadata["followup_disposition"]),
			issueDisplayKey(issue),
			stringFromAny(metadata["risk_level"]),
			ask,
			stringFromAny(metadata["source_issue_id"]),
		}
		if fullID {
			row = []string{
				fmt.Sprintf("%d", i+1),
				stringFromAny(metadata["followup_disposition"]),
				strVal(issue, "id"),
				issueDisplayKey(issue),
				stringFromAny(metadata["risk_level"]),
				ask,
				stringFromAny(metadata["source_issue_id"]),
			}
		}
		rows = append(rows, row)
	}
	cli.PrintTable(os.Stdout, headers, rows)
}
