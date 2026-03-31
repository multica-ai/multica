package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var issueCmd = &cobra.Command{
	Use:   "issue",
	Short: "Manage issues",
}

var issueListCmd = &cobra.Command{
	Use:   "list",
	Short: "List issues in the workspace",
	RunE:  runIssueList,
}

var issueGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get issue details",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueGet,
}

var issueCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new issue",
	RunE:  runIssueCreate,
}

var issueUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an issue",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueUpdate,
}

var issueAssignCmd = &cobra.Command{
	Use:   "assign <id>",
	Short: "Assign an issue to a member or agent",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueAssign,
}

var issueStatusCmd = &cobra.Command{
	Use:   "status <id> <status>",
	Short: "Change issue status",
	Args:  cobra.ExactArgs(2),
	RunE:  runIssueStatus,
}

// Comment subcommands.

var issueCommentCmd = &cobra.Command{
	Use:   "comment",
	Short: "Manage issue comments",
}

var issueCommentListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List comments on an issue",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueCommentList,
}

var issueCommentAddCmd = &cobra.Command{
	Use:   "add <issue-id>",
	Short: "Add a comment to an issue",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueCommentAdd,
}

var issueCommentDeleteCmd = &cobra.Command{
	Use:   "delete <comment-id>",
	Short: "Delete a comment",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueCommentDelete,
}

var validIssueStatuses = []string{
	"backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled",
}

func init() {
	issueCmd.AddCommand(issueListCmd)
	issueCmd.AddCommand(issueGetCmd)
	issueCmd.AddCommand(issueCreateCmd)
	issueCmd.AddCommand(issueUpdateCmd)
	issueCmd.AddCommand(issueAssignCmd)
	issueCmd.AddCommand(issueStatusCmd)
	issueCmd.AddCommand(issueCommentCmd)

	issueCommentCmd.AddCommand(issueCommentListCmd)
	issueCommentCmd.AddCommand(issueCommentAddCmd)
	issueCommentCmd.AddCommand(issueCommentDeleteCmd)

	// issue list
	issueListCmd.Flags().String("output", "table", "Output format: table or json")
	issueListCmd.Flags().String("status", "", "Filter by status")
	issueListCmd.Flags().String("priority", "", "Filter by priority")
	issueListCmd.Flags().String("assignee", "", "Filter by assignee name")
	issueListCmd.Flags().Int("limit", 50, "Maximum number of issues to return")

	// issue get
	issueGetCmd.Flags().String("output", "json", "Output format: table or json")

	// issue create
	issueCreateCmd.Flags().String("title", "", "Issue title (required)")
	issueCreateCmd.Flags().String("description", "", "Issue description")
	issueCreateCmd.Flags().String("status", "", "Issue status")
	issueCreateCmd.Flags().String("priority", "", "Issue priority")
	issueCreateCmd.Flags().String("assignee", "", "Assignee name (member or agent)")
	issueCreateCmd.Flags().String("parent", "", "Parent issue ID")
	issueCreateCmd.Flags().String("due-date", "", "Due date (RFC3339 format)")
	issueCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// issue update
	issueUpdateCmd.Flags().String("title", "", "New title")
	issueUpdateCmd.Flags().String("description", "", "New description")
	issueUpdateCmd.Flags().String("status", "", "New status")
	issueUpdateCmd.Flags().String("priority", "", "New priority")
	issueUpdateCmd.Flags().String("assignee", "", "New assignee name (member or agent)")
	issueUpdateCmd.Flags().String("due-date", "", "New due date (RFC3339 format)")
	issueUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// issue status
	issueStatusCmd.Flags().String("output", "table", "Output format: table or json")

	// issue assign
	issueAssignCmd.Flags().String("to", "", "Assignee name (member or agent)")
	issueAssignCmd.Flags().Bool("unassign", false, "Remove current assignee")
	issueAssignCmd.Flags().String("output", "json", "Output format: table or json")

	// issue comment list
	issueCommentListCmd.Flags().String("output", "table", "Output format: table or json")

	// issue comment add
	issueCommentAddCmd.Flags().String("content", "", "Comment content (required)")
	issueCommentAddCmd.Flags().String("parent", "", "Parent comment ID (reply to a specific comment)")
	issueCommentAddCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Issue commands
// ---------------------------------------------------------------------------

func runIssueList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		params.Set("status", v)
	}
	if v, _ := cmd.Flags().GetString("priority"); v != "" {
		params.Set("priority", v)
	}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetString("assignee"); v != "" {
		_, aID, resolveErr := resolveAssignee(ctx, client, v)
		if resolveErr != nil {
			return fmt.Errorf("resolve assignee: %w", resolveErr)
		}
		params.Set("assignee_id", aID)
	}

	path := "/api/issues"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list issues: %w", err)
	}

	issuesRaw, _ := result["issues"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, issuesRaw)
	}

	headers := []string{"ID", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "DUE DATE"}
	rows := make([][]string, 0, len(issuesRaw))
	for _, raw := range issuesRaw {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		assignee := formatAssignee(issue)
		dueDate := strVal(issue, "due_date")
		if dueDate != "" && len(dueDate) >= 10 {
			dueDate = dueDate[:10]
		}
		rows = append(rows, []string{
			truncateID(strVal(issue, "id")),
			strVal(issue, "title"),
			strVal(issue, "status"),
			strVal(issue, "priority"),
			assignee,
			dueDate,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var issue map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+args[0], &issue); err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		assignee := formatAssignee(issue)
		dueDate := strVal(issue, "due_date")
		if dueDate != "" && len(dueDate) >= 10 {
			dueDate = dueDate[:10]
		}
		headers := []string{"ID", "TITLE", "STATUS", "PRIORITY", "ASSIGNEE", "DUE DATE", "DESCRIPTION"}
		rows := [][]string{{
			truncateID(strVal(issue, "id")),
			strVal(issue, "title"),
			strVal(issue, "status"),
			strVal(issue, "priority"),
			assignee,
			dueDate,
			strVal(issue, "description"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, issue)
}

func runIssueCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"title": title}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		body["status"] = v
	}
	if v, _ := cmd.Flags().GetString("priority"); v != "" {
		body["priority"] = v
	}
	if v, _ := cmd.Flags().GetString("parent"); v != "" {
		body["parent_issue_id"] = v
	}
	if v, _ := cmd.Flags().GetString("due-date"); v != "" {
		body["due_date"] = v
	}
	if v, _ := cmd.Flags().GetString("assignee"); v != "" {
		aType, aID, resolveErr := resolveAssignee(ctx, client, v)
		if resolveErr != nil {
			return fmt.Errorf("resolve assignee: %w", resolveErr)
		}
		body["assignee_type"] = aType
		body["assignee_id"] = aID
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues", body, &result); err != nil {
		return fmt.Errorf("create issue: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "STATUS", "PRIORITY"}
		rows := [][]string{{
			truncateID(strVal(result, "id")),
			strVal(result, "title"),
			strVal(result, "status"),
			strVal(result, "priority"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runIssueUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{}
	if cmd.Flags().Changed("title") {
		v, _ := cmd.Flags().GetString("title")
		body["title"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if cmd.Flags().Changed("status") {
		v, _ := cmd.Flags().GetString("status")
		body["status"] = v
	}
	if cmd.Flags().Changed("priority") {
		v, _ := cmd.Flags().GetString("priority")
		body["priority"] = v
	}
	if cmd.Flags().Changed("due-date") {
		v, _ := cmd.Flags().GetString("due-date")
		body["due_date"] = v
	}
	if cmd.Flags().Changed("assignee") {
		v, _ := cmd.Flags().GetString("assignee")
		aType, aID, resolveErr := resolveAssignee(ctx, client, v)
		if resolveErr != nil {
			return fmt.Errorf("resolve assignee: %w", resolveErr)
		}
		body["assignee_type"] = aType
		body["assignee_id"] = aID
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --status, --priority, --assignee, etc.")
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update issue: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "STATUS", "PRIORITY"}
		rows := [][]string{{
			truncateID(strVal(result, "id")),
			strVal(result, "title"),
			strVal(result, "status"),
			strVal(result, "priority"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runIssueAssign(cmd *cobra.Command, args []string) error {
	toName, _ := cmd.Flags().GetString("to")
	unassign, _ := cmd.Flags().GetBool("unassign")

	if toName == "" && !unassign {
		return fmt.Errorf("provide --to <name> or --unassign")
	}
	if toName != "" && unassign {
		return fmt.Errorf("--to and --unassign are mutually exclusive")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{}
	if unassign {
		body["assignee_type"] = nil
		body["assignee_id"] = nil
	} else {
		aType, aID, resolveErr := resolveAssignee(ctx, client, toName)
		if resolveErr != nil {
			return fmt.Errorf("resolve assignee: %w", resolveErr)
		}
		body["assignee_type"] = aType
		body["assignee_id"] = aID
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+args[0], body, &result); err != nil {
		return fmt.Errorf("assign issue: %w", err)
	}

	if unassign {
		fmt.Fprintf(os.Stderr, "Issue %s unassigned.\n", truncateID(args[0]))
	} else {
		fmt.Fprintf(os.Stderr, "Issue %s assigned to %s.\n", truncateID(args[0]), toName)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssueStatus(cmd *cobra.Command, args []string) error {
	id := args[0]
	status := args[1]

	valid := false
	for _, s := range validIssueStatuses {
		if s == status {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid status %q; valid values: %s", status, strings.Join(validIssueStatuses, ", "))
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"status": status}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+id, body, &result); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Issue %s status changed to %s.\n", truncateID(id), status)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Comment commands
// ---------------------------------------------------------------------------

func runIssueCommentList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var comments []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+args[0]+"/comments", &comments); err != nil {
		return fmt.Errorf("list comments: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, comments)
	}

	headers := []string{"ID", "AUTHOR", "TYPE", "CONTENT", "CREATED"}
	rows := make([][]string, 0, len(comments))
	for _, c := range comments {
		content := strVal(c, "content")
		if utf8.RuneCountInString(content) > 80 {
			runes := []rune(content)
			content = string(runes[:77]) + "..."
		}
		created := strVal(c, "created_at")
		if len(created) >= 16 {
			created = created[:16]
		}
		rows = append(rows, []string{
			truncateID(strVal(c, "id")),
			strVal(c, "author_type") + ":" + truncateID(strVal(c, "author_id")),
			strVal(c, "type"),
			content,
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueCommentAdd(cmd *cobra.Command, args []string) error {
	content, _ := cmd.Flags().GetString("content")
	if content == "" {
		return fmt.Errorf("--content is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"content": content}
	if parentID, _ := cmd.Flags().GetString("parent"); parentID != "" {
		body["parent_id"] = parentID
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+args[0]+"/comments", body, &result); err != nil {
		return fmt.Errorf("add comment: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Comment added to issue %s.\n", truncateID(args[0]))

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runIssueCommentDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/comments/"+args[0]); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Comment %s deleted.\n", truncateID(args[0]))
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type assigneeMatch struct {
	Type string // "member" or "agent"
	ID   string // user_id for members, agent id for agents
	Name string
}

func resolveAssignee(ctx context.Context, client *cli.APIClient, name string) (string, string, error) {
	if client.WorkspaceID == "" {
		return "", "", fmt.Errorf("workspace ID is required to resolve assignees; use --workspace-id or set MULTICA_WORKSPACE_ID")
	}

	nameLower := strings.ToLower(name)
	var matches []assigneeMatch
	var errs []error

	// Search members.
	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+client.WorkspaceID+"/members", &members); err != nil {
		errs = append(errs, fmt.Errorf("fetch members: %w", err))
	} else {
		for _, m := range members {
			mName := strVal(m, "name")
			if strings.Contains(strings.ToLower(mName), nameLower) {
				matches = append(matches, assigneeMatch{
					Type: "member",
					ID:   strVal(m, "user_id"),
					Name: mName,
				})
			}
		}
	}

	// Search agents.
	var agents []map[string]any
	agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
	if err := client.GetJSON(ctx, agentPath, &agents); err != nil {
		errs = append(errs, fmt.Errorf("fetch agents: %w", err))
	} else {
		for _, a := range agents {
			aName := strVal(a, "name")
			if strings.Contains(strings.ToLower(aName), nameLower) {
				matches = append(matches, assigneeMatch{
					Type: "agent",
					ID:   strVal(a, "id"),
					Name: aName,
				})
			}
		}
	}

	// If both fetches failed, report the errors instead of a misleading "not found".
	if len(errs) == 2 {
		return "", "", fmt.Errorf("failed to resolve assignee: %v; %v", errs[0], errs[1])
	}

	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("no member or agent found matching %q", name)
	case 1:
		return matches[0].Type, matches[0].ID, nil
	default:
		var parts []string
		for _, m := range matches {
			parts = append(parts, fmt.Sprintf("  %s %q (%s)", m.Type, m.Name, truncateID(m.ID)))
		}
		return "", "", fmt.Errorf("ambiguous assignee %q; matches:\n%s", name, strings.Join(parts, "\n"))
	}
}

func formatAssignee(issue map[string]any) string {
	aType := strVal(issue, "assignee_type")
	aID := strVal(issue, "assignee_id")
	if aType == "" || aID == "" {
		return ""
	}
	return aType + ":" + truncateID(aID)
}

func truncateID(id string) string {
	if utf8.RuneCountInString(id) > 8 {
		runes := []rune(id)
		return string(runes[:8])
	}
	return id
}
