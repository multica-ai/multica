package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// View commands — read the saved issue views a member can open, and the
// issues each one lists. Writes live in cmd_view_write.go.

const viewIssuesMaxPageSize = 100

var viewCmd = &cobra.Command{
	Use:   "view",
	Short: "Read and manage saved issue views",
	Long: `Read and manage saved issue views, and list the issues they show.

A view is the caller's own or a workspace-shared saved filter. Inside an agent
run the caller is the runtime owner, so the views listed, the views created,
and any "my" scope or "assigned to me" condition are the runtime owner's.`,
}

var viewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the saved views you can open",
	RunE:  runViewList,
}

var viewGetCmd = &cobra.Command{
	Use:   "get <view>",
	Short: "Get a view's definition",
	Long:  "Get a view's definition. <view> is a view ID, ID prefix, or exact name (case-insensitive).",
	Args:  exactArgs(1),
	RunE:  runViewGet,
}

var viewIssuesCmd = &cobra.Command{
	Use:   "issues <view>",
	Short: "List the issues a view shows",
	Long: `List the issues a view shows, in the view's saved order, with every filter the
view saves applied by the server. <view> is a view ID, ID prefix, or exact name
(case-insensitive).

Pages hold at most 100 issues. While --output json reports has_more, pass its
next_cursor to --cursor for the next page; total is reported on the first page.`,
	Args: exactArgs(1),
	RunE: runViewIssues,
}

func init() {
	viewCmd.AddCommand(viewListCmd)
	viewCmd.AddCommand(viewGetCmd)
	viewCmd.AddCommand(viewIssuesCmd)

	viewListCmd.Flags().String("scope", "", "Only views of this scope: workspace, my, or project")
	viewListCmd.Flags().String("project", "", "Only views of this project (ID or ID prefix)")
	viewListCmd.Flags().String("output", "table", "Output format: table or json")
	viewListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	viewGetCmd.Flags().String("output", "json", "Output format: table or json")

	viewIssuesCmd.Flags().Int("limit", 50, fmt.Sprintf("Page size, 1 to %d", viewIssuesMaxPageSize))
	viewIssuesCmd.Flags().String("cursor", "", "Continue from the next_cursor of the previous page")
	viewIssuesCmd.Flags().String("today", "", "Calendar day (YYYY-MM-DD) that relative due-date filters resolve against (default: today in local time)")
	viewIssuesCmd.Flags().String("output", "table", "Output format: table or json")
	viewIssuesCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	viewIssuesCmd.Flags().String("fields", "", "JSON output only: comma-separated list of issue fields to include (e.g. id,identifier,title,status). Valid fields: "+strings.Join(validIssueFields, ", "))
}

func listIssueViews(ctx context.Context, client *cli.APIClient) ([]map[string]any, error) {
	if client.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace_id is required to list views")
	}
	var raw []any
	if err := client.GetJSON(ctx, "/api/issue-views?scope_type=all", &raw); err != nil {
		return nil, fmt.Errorf("list views: %w", err)
	}
	views := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if view, ok := item.(map[string]any); ok {
			views = append(views, view)
		}
	}
	return views, nil
}

// resolveIssueView finds a view by full ID, exact name (case-insensitive),
// or ID prefix, in that order.
func resolveIssueView(ctx context.Context, client *cli.APIClient, input string) (map[string]any, error) {
	ref := strings.TrimSpace(input)
	if ref == "" {
		return nil, fmt.Errorf("view is required")
	}
	if uuidRegexp.MatchString(ref) {
		var view map[string]any
		if err := client.GetJSON(ctx, "/api/issue-views/"+url.PathEscape(ref), &view); err != nil {
			return nil, err
		}
		return view, nil
	}
	views, err := listIssueViews(ctx, client)
	if err != nil {
		return nil, err
	}
	var byName []map[string]any
	for _, view := range views {
		if strings.EqualFold(strVal(view, "name"), ref) {
			byName = append(byName, view)
		}
	}
	if len(byName) == 1 {
		return byName[0], nil
	}
	if len(byName) > 1 {
		matches := make([]idCandidate, 0, len(byName))
		for _, view := range byName {
			matches = append(matches, idCandidate{ID: strVal(view, "id")})
		}
		return nil, fmt.Errorf("more than one view is named %q; pass its ID:\n%s", ref, joinCandidateIDs(matches))
	}
	prefix, err := normalizeUUIDPrefix(ref)
	if err != nil {
		return nil, fmt.Errorf("no view named %q; run `multica view list`", ref)
	}
	var matches []idCandidate
	var found map[string]any
	for _, view := range views {
		if strings.HasPrefix(compactUUID(strVal(view, "id")), prefix) {
			matches = append(matches, idCandidate{ID: strVal(view, "id")})
			found = view
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no view found matching %q; run `multica view list --full-id`", ref)
	case 1:
		return found, nil
	default:
		return nil, ambiguousIDPrefixError("view", ref, matches)
	}
}

func joinCandidateIDs(matches []idCandidate) string {
	lines := make([]string, len(matches))
	for i, m := range matches {
		lines[i] = "  " + m.ID
	}
	return strings.Join(lines, "\n")
}

// viewScopeLabel renders a view's scope as one table cell, e.g. "workspace",
// "workspace (agents)", "my (assigned)", "project 1a2b3c4d".
func viewScopeLabel(view map[string]any, fullID bool) string {
	label := strVal(view, "scope_type")
	if label == "project" {
		label += " " + displayID(strVal(view, "scope_id"), fullID)
	}
	if variant := strVal(view, "scope_variant"); variant != "" {
		label += " (" + variant + ")"
	}
	return label
}

func runViewList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	scope, _ := cmd.Flags().GetString("scope")
	if scope != "" && scope != "workspace" && scope != "my" && scope != "project" {
		return fmt.Errorf("invalid --scope %q; valid values: workspace, my, project", scope)
	}
	projectID := ""
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		if scope != "" && scope != "project" {
			return fmt.Errorf("--project lists project views; it cannot be combined with --scope %s", scope)
		}
		project, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return err
		}
		projectID = project.ID
	}

	views, err := listIssueViews(ctx, client)
	if err != nil {
		return err
	}
	shown := make([]map[string]any, 0, len(views))
	for _, view := range views {
		if scope != "" && strVal(view, "scope_type") != scope {
			continue
		}
		if projectID != "" && strVal(view, "scope_id") != projectID {
			continue
		}
		shown = append(shown, view)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, shown)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"ID", "NAME", "SCOPE", "VISIBILITY", "OWNER"}
	rows := make([][]string, 0, len(shown))
	for _, view := range shown {
		rows = append(rows, []string{
			displayID(strVal(view, "id"), fullID),
			strVal(view, "name"),
			viewScopeLabel(view, fullID),
			strVal(view, "visibility"),
			actors.actor("member", strVal(view, "owner_id")),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runViewGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	view, err := resolveIssueView(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve view: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		actors := loadActorDisplayLookup(ctx, client)
		cli.PrintTable(os.Stdout, []string{"ID", "NAME", "SCOPE", "VISIBILITY", "OWNER"}, [][]string{{
			strVal(view, "id"),
			strVal(view, "name"),
			viewScopeLabel(view, true),
			strVal(view, "visibility"),
			actors.actor("member", strVal(view, "owner_id")),
		}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, view)
}

func runViewIssues(cmd *cobra.Command, args []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 1 || limit > viewIssuesMaxPageSize {
		return fmt.Errorf("--limit must be between 1 and %d; use --cursor to page through more", viewIssuesMaxPageSize)
	}
	today, _ := cmd.Flags().GetString("today")
	if today == "" {
		today = time.Now().Format(time.DateOnly)
	} else if _, err := time.Parse(time.DateOnly, today); err != nil {
		return fmt.Errorf("invalid --today %q; use YYYY-MM-DD", today)
	}
	fields, err := parseIssueFieldsFlag(cmd)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	view, err := resolveIssueView(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve view: %w", err)
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("today", today)
	if cursor, _ := cmd.Flags().GetString("cursor"); cursor != "" {
		params.Set("cursor", cursor)
	}
	var result map[string]any
	path := "/api/issue-views/" + url.PathEscape(strVal(view, "id")) + "/issues?" + params.Encode()
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list view issues: %w", err)
	}
	issuesRaw, _ := result["issues"].([]any)
	hasMore, _ := result["has_more"].(bool)
	nextCursor := strVal(result, "next_cursor")

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		if len(fields) > 0 {
			filterIssueFields(issuesRaw, fields)
		}
		wrapped := map[string]any{
			"view":        map[string]any{"id": strVal(view, "id"), "name": strVal(view, "name")},
			"issues":      issuesRaw,
			"limit":       limit,
			"has_more":    hasMore,
			"next_cursor": result["next_cursor"],
		}
		if total, ok := result["total"]; ok {
			wrapped["total"] = total
		}
		return cli.PrintJSON(os.Stdout, wrapped)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	printIssueListTable(ctx, client, issuesRaw, fullID)
	if hasMore && nextCursor != "" {
		line := fmt.Sprintf("Showing %d issues.", len(issuesRaw))
		if total, ok := result["total"].(float64); ok {
			line = fmt.Sprintf("Showing %d of %d issues.", len(issuesRaw), int(total))
		}
		fmt.Fprintf(os.Stderr, "%s Next page: --cursor %s\n", line, nextCursor)
	}
	return nil
}
