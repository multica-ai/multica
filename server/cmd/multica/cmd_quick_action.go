package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var quickActionCmd = &cobra.Command{
	Use:     "quick-action",
	Aliases: []string{"quickaction"},
	Short:   "List, inspect, and run issue quick actions",
	Long: `List, inspect, and run issue quick actions.

Quick actions are workspace presets that post a configured prompt to an issue,
optionally targeting an agent or squad. Creating and editing them remains
human-only; the CLI exposes the list/get/run operations available to agents.`,
}

var quickActionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List quick actions visible in the current workspace",
	RunE:  runQuickActionList,
}

var quickActionGetCmd = &cobra.Command{
	Use:   "get <id-or-name>",
	Short: "Get a quick action",
	Long:  "Get a quick action by full ID, unique ID prefix, or exact name (case-insensitive). Archived actions are included.",
	Args:  exactArgs(1),
	RunE:  runQuickActionGet,
}

var quickActionRunCmd = &cobra.Command{
	Use:   "run <id-or-name> --issue <issue>",
	Short: "Run a quick action against an issue",
	Long: `Run a quick action against an issue. The issue may be an issue key such
as MUL-123 or a full issue UUID. The quick action may be a full ID, unique ID
prefix, or exact name.

The server posts the configured prompt as a comment and applies the normal
comment-to-agent trigger path. Use --dry-run to render the comment without
posting it or starting an agent task.`,
	Args: exactArgs(1),
	RunE: runQuickActionRun,
}

func init() {
	quickActionCmd.AddCommand(quickActionListCmd)
	quickActionCmd.AddCommand(quickActionGetCmd)
	quickActionCmd.AddCommand(quickActionRunCmd)

	quickActionListCmd.Flags().Bool("include-archived", false, "Include archived quick actions")
	quickActionListCmd.Flags().String("output", "table", "Output format: table or json")
	quickActionListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	quickActionGetCmd.Flags().String("output", "json", "Output format: table or json")
	quickActionGetCmd.Flags().Bool("full-id", false, "Show the full UUID in table output")

	quickActionRunCmd.Flags().String("issue", "", "Issue key or full UUID (required)")
	quickActionRunCmd.Flags().Bool("dry-run", false, "Render the prompt without posting a comment or triggering an agent")
	quickActionRunCmd.Flags().String("output", "json", "Output format: table or json")
}

type quickActionListResponse struct {
	QuickActions []map[string]any `json:"quick_actions"`
}

func runQuickActionList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	includeArchived, _ := cmd.Flags().GetBool("include-archived")
	actions, err := listQuickActions(ctx, client, includeArchived)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, quickActionListResponse{QuickActions: actions})
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	printQuickActionsTable(actions, fullID)
	return nil
}

func runQuickActionGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	action, err := resolveQuickAction(ctx, client, args[0])
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, action)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	printQuickActionsTable([]map[string]any{action}, fullID)
	return nil
}

func runQuickActionRun(cmd *cobra.Command, args []string) error {
	issueRef, _ := cmd.Flags().GetString("issue")
	if strings.TrimSpace(issueRef) == "" {
		return fmt.Errorf("--issue is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	actionID, err := resolveQuickActionID(ctx, client, args[0])
	if err != nil {
		return err
	}
	issue, err := resolveIssueRef(ctx, client, issueRef)
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	path := "/api/issues/" + url.PathEscape(issue.ID) + "/quick-actions/" + url.PathEscape(actionID.ID)
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		path += "/render"
	} else {
		path += "/run"
	}

	var result map[string]any
	if err := client.PostJSON(ctx, path, map[string]any{}, &result); err != nil {
		if dryRun {
			return fmt.Errorf("render quick action: %w", err)
		}
		return fmt.Errorf("run quick action: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	if dryRun {
		content := strVal(result, "content")
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		fmt.Fprint(os.Stdout, content)
		return nil
	}

	cli.PrintTable(os.Stdout,
		[]string{"COMMENT", "ISSUE", "OUTCOMES"},
		[][]string{{
			displayID(strVal(result, "id"), false),
			strVal(result, "issue_id"),
			quickActionOutcomeSummary(result["trigger_outcomes"]),
		}},
	)
	return nil
}

func listQuickActions(ctx context.Context, client *cli.APIClient, includeArchived bool) ([]map[string]any, error) {
	path := "/api/quick-actions"
	if includeArchived {
		path += "?" + url.Values{"include_archived": {"true"}}.Encode()
	}

	var resp quickActionListResponse
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("list quick actions: %w", err)
	}
	if resp.QuickActions == nil {
		resp.QuickActions = []map[string]any{}
	}
	return resp.QuickActions, nil
}

func resolveQuickAction(ctx context.Context, client *cli.APIClient, input string) (map[string]any, error) {
	actions, err := listQuickActions(ctx, client, true)
	if err != nil {
		return nil, err
	}
	ref := strings.TrimSpace(input)
	if ref == "" {
		return nil, fmt.Errorf("quick action is required")
	}

	if uuidRegexp.MatchString(ref) {
		for _, action := range actions {
			if strings.EqualFold(strVal(action, "id"), ref) {
				return action, nil
			}
		}
		return nil, fmt.Errorf("quick action %q not found or not visible to this caller", ref)
	}

	var named []map[string]any
	for _, action := range actions {
		if strings.EqualFold(strVal(action, "name"), ref) {
			named = append(named, action)
		}
	}
	if len(named) == 1 {
		return named[0], nil
	}
	if len(named) > 1 {
		candidates := make([]idCandidate, 0, len(named))
		for _, action := range named {
			candidates = append(candidates, idCandidate{ID: strVal(action, "id"), Display: strVal(action, "name")})
		}
		return nil, fmt.Errorf("more than one quick action is named %q; pass its ID:\n%s", ref, joinCandidateIDs(candidates))
	}

	prefix, err := normalizeUUIDPrefix(ref)
	if err != nil {
		return nil, fmt.Errorf("no quick action named %q; run `multica quick-action list --full-id --include-archived`", ref)
	}
	var matches []map[string]any
	for _, action := range actions {
		if strings.HasPrefix(compactUUID(strVal(action, "id")), prefix) {
			matches = append(matches, action)
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no quick action found matching %q; run `multica quick-action list --full-id --include-archived`", ref)
	case 1:
		return matches[0], nil
	default:
		candidates := make([]idCandidate, 0, len(matches))
		for _, action := range matches {
			candidates = append(candidates, idCandidate{ID: strVal(action, "id"), Display: strVal(action, "name")})
		}
		return nil, ambiguousIDPrefixError("quick action", ref, candidates)
	}
}

func resolveQuickActionID(ctx context.Context, client *cli.APIClient, input string) (resolvedID, error) {
	ref := strings.TrimSpace(input)
	if uuidRegexp.MatchString(ref) {
		return resolvedID{ID: strings.ToLower(ref), Display: ref}, nil
	}
	action, err := resolveQuickAction(ctx, client, ref)
	if err != nil {
		return resolvedID{}, err
	}
	return resolvedID{ID: strVal(action, "id"), Display: strVal(action, "name")}, nil
}

func printQuickActionsTable(actions []map[string]any, fullID bool) {
	headers := []string{"ID", "NAME", "STATUS", "VISIBILITY", "TARGET", "USES", "LAST USED"}
	rows := make([][]string, 0, len(actions))
	for _, action := range actions {
		rows = append(rows, []string{
			displayID(strVal(action, "id"), fullID),
			strVal(action, "name"),
			strVal(action, "status"),
			strVal(action, "visibility"),
			quickActionTargetLabel(action),
			strVal(action, "use_count"),
			quickActionLastUsed(action),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func quickActionTargetLabel(action map[string]any) string {
	if missing, _ := action["target_missing"].(bool); missing {
		return "missing"
	}
	targetType := strVal(action, "assignee_type")
	if targetType == "" {
		return "(none)"
	}
	targetName := strVal(action, "target_name")
	if targetName == "" {
		targetName = displayID(strVal(action, "assignee_id"), false)
	}
	return targetType + ":" + targetName
}

func quickActionLastUsed(action map[string]any) string {
	raw := strings.TrimSpace(strVal(action, "last_used_at"))
	if raw == "" {
		return "never"
	}
	return relativeTimestamp(raw)
}

func quickActionOutcomeSummary(raw any) string {
	outcomes, ok := raw.([]any)
	if !ok || len(outcomes) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(outcomes))
	for _, item := range outcomes {
		outcome, ok := item.(map[string]any)
		if !ok {
			continue
		}
		targetType := strVal(outcome, "target_type")
		targetID := displayID(strVal(outcome, "target_id"), false)
		status := strVal(outcome, "status")
		reason := strVal(outcome, "reason_code")
		if reason != "" {
			status += "(" + reason + ")"
		}
		parts = append(parts, targetType+":"+targetID+"="+status)
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}
