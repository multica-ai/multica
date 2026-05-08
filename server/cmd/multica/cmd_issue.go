package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/util"
)

// resolveTextFlag picks between a `--<name>` flag value and a paired
// `--<name>-stdin` flag, mirroring the existing `--content` / `--content-stdin`
// pattern. It returns the resolved string and an error when both are set or
// stdin is requested but produces no body. Inline flag values are passed
// through util.UnescapeBackslashEscapes so bash-double-quoted `\n` becomes a
// real newline; stdin bodies are returned verbatim so literal backslashes
// survive intact.
func resolveTextFlag(cmd *cobra.Command, flagName string) (string, bool, error) {
	stdinFlag := flagName + "-stdin"
	useStdin, _ := cmd.Flags().GetBool(stdinFlag)
	inline, _ := cmd.Flags().GetString(flagName)
	if useStdin && inline != "" {
		return "", false, fmt.Errorf("--%s and --%s are mutually exclusive", flagName, stdinFlag)
	}
	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", false, fmt.Errorf("read stdin for --%s: %w", stdinFlag, err)
		}
		body := strings.TrimSuffix(string(data), "\n")
		if body == "" {
			return "", false, fmt.Errorf("stdin content for --%s is empty", stdinFlag)
		}
		return body, true, nil
	}
	if inline == "" {
		return "", false, nil
	}
	return util.UnescapeBackslashEscapes(inline), true, nil
}

var issueCmd = &cobra.Command{
	Use:   "issue",
	Short: "Work with issues",
}

var issueListCmd = &cobra.Command{
	Use:   "list",
	Short: "List issues in the workspace",
	RunE:  runIssueList,
}

var issueGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get issue details",
	Args:  exactArgs(1),
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
	Args:  exactArgs(1),
	RunE:  runIssueUpdate,
}

var issueAssignCmd = &cobra.Command{
	Use:   "assign <id>",
	Short: "Assign an issue to a member or agent",
	Args:  exactArgs(1),
	RunE:  runIssueAssign,
}

var issueStatusCmd = &cobra.Command{
	Use:   "status <id> <status>",
	Short: "Change issue status",
	Args:  exactArgs(2),
	RunE:  runIssueStatus,
}

// Comment subcommands.

var issueCommentCmd = &cobra.Command{
	Use:   "comment",
	Short: "Work with issue comments",
}

var issueCommentListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List comments on an issue",
	Args:  exactArgs(1),
	RunE:  runIssueCommentList,
}

var issueCommentAddCmd = &cobra.Command{
	Use:   "add <issue-id>",
	Short: "Add a comment to an issue",
	Args:  exactArgs(1),
	RunE:  runIssueCommentAdd,
}

var issueCommentDeleteCmd = &cobra.Command{
	Use:   "delete <comment-id>",
	Short: "Delete a comment",
	Args:  exactArgs(1),
	RunE:  runIssueCommentDelete,
}

// Subscriber subcommands.

var issueSubscriberCmd = &cobra.Command{
	Use:   "subscriber",
	Short: "Work with issue subscribers",
}

var issueSubscriberListCmd = &cobra.Command{
	Use:   "list <issue-id>",
	Short: "List subscribers of an issue",
	Args:  exactArgs(1),
	RunE:  runIssueSubscriberList,
}

var issueSubscriberAddCmd = &cobra.Command{
	Use:   "add <issue-id>",
	Short: "Subscribe a user or agent to an issue (defaults to the caller)",
	Args:  exactArgs(1),
	RunE:  runIssueSubscriberAdd,
}

var issueSubscriberRemoveCmd = &cobra.Command{
	Use:   "remove <issue-id>",
	Short: "Unsubscribe a user or agent from an issue (defaults to the caller)",
	Args:  exactArgs(1),
	RunE:  runIssueSubscriberRemove,
}

// Execution history subcommands.

var issueRunsCmd = &cobra.Command{
	Use:   "runs <issue-id>",
	Short: "List execution history for an issue",
	Args:  exactArgs(1),
	RunE:  runIssueRuns,
}

var issueRunMessagesCmd = &cobra.Command{
	Use:   "run-messages <task-id>",
	Short: "List messages for an execution",
	Args:  exactArgs(1),
	RunE:  runIssueRunMessages,
}

var issueRerunCmd = &cobra.Command{
	Use:   "rerun <id>",
	Short: "Re-enqueue an issue's current agent assignment as a fresh task",
	Args:  exactArgs(1),
	RunE:  runIssueRerun,
}

var issueTakeCmd = &cobra.Command{
	Use:   "take <issue-id>",
	Short: "Resume an agent's last session for an issue locally",
	Long: `Take over an issue's most recent completed agent run on this machine.

By default this spawns the matching agent CLI inside the worktree the
agent used, with stdin/stdout/stderr wired through. The agent picks up
the same session, so prior context is preserved.

Use --print to emit the equivalent shell command for use with eval, or
--copy to drop it on the system clipboard.

Local runtimes only. Cloud-runtime tasks do not persist a local
session_id + work_dir, so the latest run is skipped and the command
exits with "no completed run found". A future remote-takeover feature
would need its own attach/exec transport — see the issue tracker.`,
	Args: exactArgs(1),
	RunE: runIssueTake,
}

var issueSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search issues by title or description",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueSearch,
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
	issueCmd.AddCommand(issueSubscriberCmd)
	issueCmd.AddCommand(issueRunsCmd)
	issueCmd.AddCommand(issueRunMessagesCmd)
	issueCmd.AddCommand(issueRerunCmd)
	issueCmd.AddCommand(issueTakeCmd)
	issueCmd.AddCommand(issueSearchCmd)

	issueCommentCmd.AddCommand(issueCommentListCmd)
	issueCommentCmd.AddCommand(issueCommentAddCmd)
	issueCommentCmd.AddCommand(issueCommentDeleteCmd)

	issueSubscriberCmd.AddCommand(issueSubscriberListCmd)
	issueSubscriberCmd.AddCommand(issueSubscriberAddCmd)
	issueSubscriberCmd.AddCommand(issueSubscriberRemoveCmd)

	// issue list
	issueListCmd.Flags().String("output", "table", "Output format: table or json")
	issueListCmd.Flags().String("status", "", "Filter by status")
	issueListCmd.Flags().String("priority", "", "Filter by priority")
	issueListCmd.Flags().String("assignee", "", "Filter by assignee name (member or agent; fuzzy match)")
	issueListCmd.Flags().String("assignee-id", "", "Filter by assignee UUID (mutually exclusive with --assignee)")
	issueListCmd.Flags().String("project", "", "Filter by project ID")
	issueListCmd.Flags().Int("limit", 50, "Maximum number of issues to return")
	issueListCmd.Flags().Int("offset", 0, "Number of issues to skip (for pagination)")

	// issue get
	issueGetCmd.Flags().String("output", "json", "Output format: table or json")

	// issue create
	issueCreateCmd.Flags().String("title", "", "Issue title (required)")
	issueCreateCmd.Flags().String("description", "", "Issue description (decodes \\n, \\r, \\t, \\\\; pipe via --description-stdin to preserve literal backslashes)")
	issueCreateCmd.Flags().Bool("description-stdin", false, "Read issue description from stdin (preserves multi-line content verbatim)")
	issueCreateCmd.Flags().String("status", "", "Issue status")
	issueCreateCmd.Flags().String("priority", "", "Issue priority")
	issueCreateCmd.Flags().String("assignee", "", "Assignee name (member or agent; fuzzy match)")
	issueCreateCmd.Flags().String("assignee-id", "", "Assignee UUID (mutually exclusive with --assignee)")
	issueCreateCmd.Flags().String("parent", "", "Parent issue ID")
	issueCreateCmd.Flags().String("project", "", "Project ID")
	issueCreateCmd.Flags().String("due-date", "", "Due date (RFC3339 format)")
	issueCreateCmd.Flags().String("output", "json", "Output format: table or json")
	issueCreateCmd.Flags().StringSlice("attachment", nil, "File path(s) to attach (can be specified multiple times)")

	// issue update
	issueUpdateCmd.Flags().String("title", "", "New title")
	issueUpdateCmd.Flags().String("description", "", "New description (decodes \\n, \\r, \\t, \\\\; pipe via --description-stdin to preserve literal backslashes)")
	issueUpdateCmd.Flags().Bool("description-stdin", false, "Read new description from stdin (preserves multi-line content verbatim)")
	issueUpdateCmd.Flags().String("status", "", "New status")
	issueUpdateCmd.Flags().String("priority", "", "New priority")
	issueUpdateCmd.Flags().String("assignee", "", "New assignee name (member or agent; fuzzy match)")
	issueUpdateCmd.Flags().String("assignee-id", "", "New assignee UUID (mutually exclusive with --assignee)")
	issueUpdateCmd.Flags().String("project", "", "Project ID")
	issueUpdateCmd.Flags().String("due-date", "", "New due date (RFC3339 format)")
	issueUpdateCmd.Flags().String("parent", "", "Parent issue ID (use --parent \"\" to clear)")
	issueUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// issue status
	issueStatusCmd.Flags().String("output", "table", "Output format: table or json")

	// issue assign
	issueAssignCmd.Flags().String("to", "", "Assignee name (member or agent; fuzzy match)")
	issueAssignCmd.Flags().String("to-id", "", "Assignee UUID (mutually exclusive with --to)")
	issueAssignCmd.Flags().Bool("unassign", false, "Remove current assignee")
	issueAssignCmd.Flags().String("output", "json", "Output format: table or json")

	// issue comment list
	issueCommentListCmd.Flags().String("output", "table", "Output format: table or json")
	issueCommentListCmd.Flags().Int("limit", 50, "Maximum number of comments to return (0 = server default of 50, max ~100 depending on server)")
	issueCommentListCmd.Flags().Int("offset", 0, "Number of comments to skip")
	issueCommentListCmd.Flags().String("since", "", "Only return comments created after this timestamp (RFC3339)")

	// issue runs
	issueRunsCmd.Flags().String("output", "table", "Output format: table or json")

	// issue rerun
	issueRerunCmd.Flags().String("output", "json", "Output format: table or json")

	// issue take
	issueTakeCmd.Flags().String("provider", "", "Force a specific provider (claude, codex, cursor, gemini, opencode, copilot); auto-detected from the latest run when omitted")
	issueTakeCmd.Flags().Bool("print", false, "Print the shell command to stdout instead of executing it (use with eval)")
	issueTakeCmd.Flags().Bool("copy", false, "Copy the shell command to the system clipboard instead of executing it")

	// issue run-messages
	issueRunMessagesCmd.Flags().String("output", "json", "Output format: table or json")
	issueRunMessagesCmd.Flags().Int("since", 0, "Only return messages after this sequence number")

	// issue comment add
	issueCommentAddCmd.Flags().String("content", "", "Comment content (decodes \\n, \\r, \\t, \\\\; pipe via --content-stdin for multi-line bodies or to preserve literal backslashes)")
	issueCommentAddCmd.Flags().Bool("content-stdin", false, "Read comment content from stdin (preserves multi-line content verbatim)")
	issueCommentAddCmd.Flags().String("parent", "", "Parent comment ID (reply to a specific comment)")
	issueCommentAddCmd.Flags().StringSlice("attachment", nil, "File path(s) to attach (can be specified multiple times)")
	issueCommentAddCmd.Flags().String("output", "json", "Output format: table or json")

	// issue search
	issueSearchCmd.Flags().Int("limit", 20, "Maximum number of results to return")
	issueSearchCmd.Flags().Bool("include-closed", false, "Include done and cancelled issues")
	issueSearchCmd.Flags().String("output", "table", "Output format: table or json")

	// issue subscriber list
	issueSubscriberListCmd.Flags().String("output", "table", "Output format: table or json")

	// issue subscriber add
	issueSubscriberAddCmd.Flags().String("user", "", "Member or agent name to subscribe (fuzzy match; defaults to the caller)")
	issueSubscriberAddCmd.Flags().String("user-id", "", "Member or agent UUID to subscribe (mutually exclusive with --user)")
	issueSubscriberAddCmd.Flags().String("output", "json", "Output format: table or json")

	// issue subscriber remove
	issueSubscriberRemoveCmd.Flags().String("user", "", "Member or agent name to unsubscribe (fuzzy match; defaults to the caller)")
	issueSubscriberRemoveCmd.Flags().String("user-id", "", "Member or agent UUID to unsubscribe (mutually exclusive with --user)")
	issueSubscriberRemoveCmd.Flags().String("output", "json", "Output format: table or json")
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

	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}

	params := url.Values{}
	params.Set("workspace_id", client.WorkspaceID)
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		params.Set("status", v)
	}
	if v, _ := cmd.Flags().GetString("priority"); v != "" {
		params.Set("priority", v)
	}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	_, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id")
	if resolveErr != nil {
		return fmt.Errorf("resolve assignee: %w", resolveErr)
	}
	if hasAssignee {
		params.Set("assignee_id", aID)
	}
	if v, _ := cmd.Flags().GetInt("offset"); v > 0 {
		params.Set("offset", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		params.Set("project_id", v)
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
		total, _ := result["total"].(float64)
		limit, _ := cmd.Flags().GetInt("limit")
		offset, _ := cmd.Flags().GetInt("offset")
		hasMore := offset+len(issuesRaw) < int(total)
		wrapped := map[string]any{
			"issues":   issuesRaw,
			"total":    int(total),
			"limit":    limit,
			"offset":   offset,
			"has_more": hasMore,
		}
		return cli.PrintJSON(os.Stdout, wrapped)
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

// isHTTPURL reports whether path is an http:// or https:// URL.
// Used to skip URL-shaped values passed to --attachment, which only
// accepts local file paths. Trims surrounding whitespace because
// agent-generated commands sometimes copy URLs with stray spaces.
func isHTTPURL(path string) bool {
	p := strings.TrimSpace(path)
	return strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://")
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

	// Use a longer timeout when attachments are present (file uploads can be slow).
	timeout := 15 * time.Second
	attachments, _ := cmd.Flags().GetStringSlice("attachment")
	if len(attachments) > 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	body := map[string]any{"title": title}
	desc, hasDesc, err := resolveTextFlag(cmd, "description")
	if err != nil {
		return err
	}
	if hasDesc {
		body["description"] = desc
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
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		body["project_id"] = v
	}
	if v, _ := cmd.Flags().GetString("due-date"); v != "" {
		body["due_date"] = v
	}
	aType, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id")
	if resolveErr != nil {
		return fmt.Errorf("resolve assignee: %w", resolveErr)
	}
	if hasAssignee {
		body["assignee_type"] = aType
		body["assignee_id"] = aID
	}

	// Quick-create stamp: when the daemon sets MULTICA_QUICK_CREATE_TASK_ID
	// before invoking the agent, the agent's `multica issue create` call
	// inherits the env var and tags the new issue with origin_type=
	// quick_create + origin_id=<task_id>. The completion handler then
	// locates the issue deterministically by origin instead of "most
	// recent issue by this agent", which is racy when max_concurrent_tasks
	// > 1 and the agent is creating other issues in parallel.
	if taskID := os.Getenv("MULTICA_QUICK_CREATE_TASK_ID"); taskID != "" {
		body["origin_type"] = "quick_create"
		body["origin_id"] = taskID
	}

	// Pre-validate attachments BEFORE creating the issue so a bad path
	// can never produce a half-created issue (which would otherwise
	// trigger callers — especially the agent doing quick-create — to
	// retry the whole `issue create` and end up with duplicates).
	//
	//   - http(s) URLs are not local files; the API only accepts local
	//     paths here. Warn and skip rather than fail — a markdown image
	//     URL embedded in the prompt should never be re-attached, and
	//     skipping is the safest outcome for that case.
	//   - Anything else is treated as a local path and read upfront.
	//     A read failure here is a real user/agent mistake (typo,
	//     missing file) and we surface it pre-create so the issue
	//     never lands.
	type pendingAttachment struct {
		path string
		data []byte
	}
	pending := make([]pendingAttachment, 0, len(attachments))
	for _, filePath := range attachments {
		if isHTTPURL(filePath) {
			fmt.Fprintf(os.Stderr, "Skipping --attachment %q: URLs are not supported here, only local file paths.\n", filePath)
			continue
		}
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return fmt.Errorf("read attachment %s: %w", filePath, readErr)
		}
		pending = append(pending, pendingAttachment{path: filePath, data: data})
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues", body, &result); err != nil {
		return fmt.Errorf("create issue: %w", err)
	}

	// Upload attachments and link them to the newly created issue.
	// Failures here are partial-success: the issue exists already, so
	// turning a non-zero exit on the caller would invite a retry that
	// duplicates the issue. Warn on stderr and continue.
	issueID := strVal(result, "id")
	for _, att := range pending {
		if _, uploadErr := client.UploadFile(ctx, att.data, att.path, issueID); uploadErr != nil {
			fmt.Fprintf(os.Stderr, "warning: upload attachment %s failed (issue already created, %s): %v\n",
				att.path, strVal(result, "identifier"), uploadErr)
			continue
		}
		fmt.Fprintf(os.Stderr, "Uploaded %s\n", att.path)
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
	if cmd.Flags().Changed("description") || cmd.Flags().Changed("description-stdin") {
		desc, _, err := resolveTextFlag(cmd, "description")
		if err != nil {
			return err
		}
		body["description"] = desc
	}
	if cmd.Flags().Changed("status") {
		v, _ := cmd.Flags().GetString("status")
		body["status"] = v
	}
	if cmd.Flags().Changed("priority") {
		v, _ := cmd.Flags().GetString("priority")
		body["priority"] = v
	}
	if cmd.Flags().Changed("project") {
		v, _ := cmd.Flags().GetString("project")
		body["project_id"] = v
	}
	if cmd.Flags().Changed("due-date") {
		v, _ := cmd.Flags().GetString("due-date")
		body["due_date"] = v
	}
	if cmd.Flags().Changed("assignee") || cmd.Flags().Changed("assignee-id") {
		aType, aID, hasAssignee, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "assignee", "assignee-id")
		if resolveErr != nil {
			return fmt.Errorf("resolve assignee: %w", resolveErr)
		}
		if hasAssignee {
			body["assignee_type"] = aType
			body["assignee_id"] = aID
		}
	}
	if cmd.Flags().Changed("parent") {
		v, _ := cmd.Flags().GetString("parent")
		if v == "" {
			body["parent_issue_id"] = nil
		} else {
			body["parent_issue_id"] = v
		}
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
	toNameSet := cmd.Flags().Changed("to")
	toIDSet := cmd.Flags().Changed("to-id")

	if !toNameSet && !toIDSet && !unassign {
		return fmt.Errorf("provide --to <name>, --to-id <uuid>, or --unassign")
	}
	if (toNameSet || toIDSet) && unassign {
		return fmt.Errorf("--to/--to-id and --unassign are mutually exclusive")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{}
	displayTarget := toName
	if unassign {
		body["assignee_type"] = nil
		body["assignee_id"] = nil
	} else {
		aType, aID, _, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "to", "to-id")
		if resolveErr != nil {
			return fmt.Errorf("resolve assignee: %w", resolveErr)
		}
		body["assignee_type"] = aType
		body["assignee_id"] = aID
		if displayTarget == "" {
			displayTarget = truncateID(aID)
		}
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+args[0], body, &result); err != nil {
		return fmt.Errorf("assign issue: %w", err)
	}

	if unassign {
		fmt.Fprintf(os.Stderr, "Issue %s unassigned.\n", truncateID(args[0]))
	} else {
		fmt.Fprintf(os.Stderr, "Issue %s assigned to %s.\n", truncateID(args[0]), displayTarget)
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

	params := url.Values{}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetInt("offset"); v > 0 {
		params.Set("offset", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetString("since"); v != "" {
		params.Set("since", v)
	}

	path := "/api/issues/" + args[0] + "/comments"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var comments []map[string]any
	isPaginated := len(params) > 0
	if isPaginated {
		headers, getErr := client.GetJSONWithHeaders(ctx, path, &comments)
		if getErr != nil {
			return fmt.Errorf("list comments: %w", getErr)
		}
		if total := headers.Get("X-Total-Count"); total != "" {
			fmt.Fprintf(os.Stderr, "Showing %d of %s comments.\n", len(comments), total)
		}
	} else {
		if err := client.GetJSON(ctx, path, &comments); err != nil {
			return fmt.Errorf("list comments: %w", err)
		}
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, comments)
	}

	headers := []string{"ID", "PARENT", "AUTHOR", "TYPE", "CONTENT", "CREATED"}
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
		parentID := strVal(c, "parent_id")
		if parentID == "" {
			parentID = "—"
		}
		rows = append(rows, []string{
			strVal(c, "id"),
			parentID,
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
	content, hasContent, err := resolveTextFlag(cmd, "content")
	if err != nil {
		return err
	}
	if !hasContent {
		return fmt.Errorf("--content or --content-stdin is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	issueID := args[0]

	// Use a longer timeout when attachments are present (file uploads can be slow).
	timeout := 15 * time.Second
	attachments, _ := cmd.Flags().GetStringSlice("attachment")
	if len(attachments) > 0 {
		timeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Upload attachments and collect their IDs. URLs are skipped with a
	// warning — `--attachment` only accepts local file paths, and a
	// markdown image URL embedded in agent-supplied content should never
	// be re-uploaded as if it were a file. Unlike `issue create`, this
	// path uploads BEFORE posting the comment, so a hard failure on a
	// real (local) attachment correctly aborts the whole call.
	var attachmentIDs []string
	for _, filePath := range attachments {
		if isHTTPURL(filePath) {
			fmt.Fprintf(os.Stderr, "Skipping --attachment %q: URLs are not supported here, only local file paths.\n", filePath)
			continue
		}
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return fmt.Errorf("read attachment %s: %w", filePath, readErr)
		}
		id, uploadErr := client.UploadFile(ctx, data, filePath, issueID)
		if uploadErr != nil {
			return fmt.Errorf("upload attachment %s: %w", filePath, uploadErr)
		}
		attachmentIDs = append(attachmentIDs, id)
		fmt.Fprintf(os.Stderr, "Uploaded %s\n", filePath)
	}

	body := map[string]any{"content": content}
	if parentID, _ := cmd.Flags().GetString("parent"); parentID != "" {
		body["parent_id"] = parentID
	}
	if len(attachmentIDs) > 0 {
		body["attachment_ids"] = attachmentIDs
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+issueID+"/comments", body, &result); err != nil {
		return fmt.Errorf("add comment: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Comment added to issue %s.\n", truncateID(issueID))

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
// Execution history commands
// ---------------------------------------------------------------------------

func runIssueRuns(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var runs []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+args[0]+"/task-runs", &runs); err != nil {
		return fmt.Errorf("list runs: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, runs)
	}

	headers := []string{"ID", "AGENT", "STATUS", "STARTED", "COMPLETED", "ERROR"}
	rows := make([][]string, 0, len(runs))
	for _, r := range runs {
		started := strVal(r, "started_at")
		if len(started) >= 16 {
			started = started[:16]
		}
		completed := strVal(r, "completed_at")
		if len(completed) >= 16 {
			completed = completed[:16]
		}
		errMsg := strVal(r, "error")
		if utf8.RuneCountInString(errMsg) > 50 {
			runes := []rune(errMsg)
			errMsg = string(runes[:47]) + "..."
		}
		rows = append(rows, []string{
			truncateID(strVal(r, "id")),
			truncateID(strVal(r, "agent_id")),
			strVal(r, "status"),
			started,
			completed,
			errMsg,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueRunMessages(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/daemon/tasks/" + args[0] + "/messages"
	if since, _ := cmd.Flags().GetInt("since"); since > 0 {
		path += fmt.Sprintf("?since=%d", since)
	}

	var messages []map[string]any
	if err := client.GetJSON(ctx, path, &messages); err != nil {
		return fmt.Errorf("list run messages: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, messages)
	}

	headers := []string{"SEQ", "TYPE", "TOOL", "CONTENT"}
	rows := make([][]string, 0, len(messages))
	for _, m := range messages {
		content := strVal(m, "content")
		if content == "" {
			content = strVal(m, "output")
		}
		if utf8.RuneCountInString(content) > 80 {
			runes := []rune(content)
			content = string(runes[:77]) + "..."
		}
		seq := ""
		if v, ok := m["seq"]; ok {
			seq = fmt.Sprintf("%v", v)
		}
		rows = append(rows, []string{
			seq,
			strVal(m, "type"),
			strVal(m, "tool"),
			content,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// Search command
// ---------------------------------------------------------------------------

func runIssueRerun(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var task map[string]any
	if err := client.PostJSON(ctx, "/api/issues/"+args[0]+"/rerun", map[string]any{}, &task); err != nil {
		return fmt.Errorf("rerun issue: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, task)
	}
	fmt.Fprintf(os.Stdout, "Re-enqueued task %s on agent %s\n", strVal(task, "id"), strVal(task, "agent_id"))
	return nil
}

// ---------------------------------------------------------------------------
// Take command — local agent takeover
// ---------------------------------------------------------------------------

// providerResume describes how to resume a session for a given provider.
// Captured as a small struct (not just a single string) because the
// flag-vs-subcommand split varies per CLI: claude uses `--resume <id>`,
// codex uses a `resume <id>` subcommand, gemini uses `-r <id>`, etc.
type providerResume struct {
	Bin  string
	Args []string
}

// resumeCommandForProvider maps a runtime provider to the local CLI
// invocation that resumes the given session. Returns an error for
// providers we have not mapped — callers translate that into a
// non-zero exit per the take command spec.
func resumeCommandForProvider(provider, sessionID string) (providerResume, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "claude":
		return providerResume{Bin: "claude", Args: []string{"--resume", sessionID}}, nil
	case "codex":
		return providerResume{Bin: "codex", Args: []string{"resume", sessionID}}, nil
	case "cursor":
		return providerResume{Bin: "cursor-agent", Args: []string{"--resume", sessionID}}, nil
	case "gemini":
		return providerResume{Bin: "gemini", Args: []string{"-r", sessionID}}, nil
	case "opencode":
		return providerResume{Bin: "opencode", Args: []string{"--session", sessionID}}, nil
	case "copilot":
		return providerResume{Bin: "copilot", Args: []string{"--resume", sessionID}}, nil
	default:
		return providerResume{}, fmt.Errorf("unsupported provider %q", provider)
	}
}

// shellSingleQuote wraps s in POSIX single quotes, escaping any embedded
// single quotes so the result is safe to paste into bash/zsh. We use
// single quotes so the workdir path and session id are passed through
// without any expansion.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// buildShellResumeCommand renders the user-pasteable command line for the
// given workdir + resume invocation. When workdir is empty, we omit the
// `cd` prefix so callers can still get a usable command after a GC.
func buildShellResumeCommand(workDir string, r providerResume) string {
	parts := make([]string, 0, len(r.Args)+1)
	parts = append(parts, r.Bin)
	for _, a := range r.Args {
		parts = append(parts, shellSingleQuote(a))
	}
	cmd := strings.Join(parts, " ")
	if workDir == "" {
		return cmd
	}
	return "cd " + shellSingleQuote(workDir) + " && " + cmd
}

// pickResumableRun walks task-runs (newest first) and returns the first
// completed run that carries a session_id. Returns ok=false when nothing
// usable exists. work_dir may legitimately be empty and is not required
// for matching — the agent CLI can still resume a session by id.
func pickResumableRun(runs []map[string]any) (sessionID, workDir, runtimeID string, ok bool) {
	for _, r := range runs {
		if strVal(r, "status") != "completed" {
			continue
		}
		result, _ := r["result"].(map[string]any)
		sid := strVal(result, "session_id")
		if sid == "" {
			continue
		}
		return sid, strVal(result, "work_dir"), strVal(r, "runtime_id"), true
	}
	return "", "", "", false
}

// copyToClipboard pipes s into the platform's clipboard utility. macOS
// has pbcopy in the base system; Linux requires xclip or xsel — we try
// xclip first then fall back, matching what the take spec lists.
func copyToClipboard(ctx context.Context, s string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "pbcopy")
	case "linux":
		if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.CommandContext(ctx, "xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.CommandContext(ctx, "xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("no clipboard tool found; install xclip or xsel")
		}
	default:
		return fmt.Errorf("clipboard copy not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

func runIssueTake(cmd *cobra.Command, args []string) error {
	printMode, _ := cmd.Flags().GetBool("print")
	copyMode, _ := cmd.Flags().GetBool("copy")
	if printMode && copyMode {
		return fmt.Errorf("--print and --copy are mutually exclusive")
	}
	forceProvider, _ := cmd.Flags().GetString("provider")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var runs []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+args[0]+"/task-runs", &runs); err != nil {
		return fmt.Errorf("list runs: %w", err)
	}

	sessionID, workDir, runtimeID, ok := pickResumableRun(runs)
	if !ok {
		return fmt.Errorf("no completed run found for issue %s", args[0])
	}

	provider := strings.TrimSpace(forceProvider)
	if provider == "" {
		var runtimes []map[string]any
		if err := client.GetJSON(ctx, "/api/runtimes", &runtimes); err != nil {
			return fmt.Errorf("list runtimes: %w", err)
		}
		for _, rt := range runtimes {
			if strVal(rt, "id") == runtimeID {
				provider = strVal(rt, "provider")
				break
			}
		}
		if provider == "" {
			return fmt.Errorf("runtime %s not found in this workspace; pass --provider explicitly", truncateID(runtimeID))
		}
	}

	resume, providerErr := resumeCommandForProvider(provider, sessionID)
	if providerErr != nil {
		// Spec: print the raw command via --print style output and exit non-zero.
		// We have no canonical command for this provider, so surface what we know
		// (workdir + session id) so the user can assemble it themselves.
		fmt.Fprintf(os.Stderr, "Provider %q is not in the supported take list. Resume manually with:\n", provider)
		if workDir != "" {
			fmt.Fprintf(os.Stderr, "  cd %s\n", shellSingleQuote(workDir))
		}
		fmt.Fprintf(os.Stderr, "  session_id: %s\n", sessionID)
		return providerErr
	}

	// If the workdir was GC'd we still want the resume to work — most
	// agent CLIs key off the session id, not the directory. Drop into
	// the user's current shell cwd (i.e. clear Cmd.Dir) and warn so
	// they know the file context they had may be gone.
	workdirUsable := workDir != ""
	if workdirUsable {
		if _, statErr := os.Stat(workDir); statErr != nil {
			workdirUsable = false
			fmt.Fprintf(os.Stderr, "warning: worktree %s is gone; resuming session from current directory.\n", workDir)
		}
	}

	if printMode {
		effectiveDir := workDir
		if !workdirUsable {
			effectiveDir = ""
		}
		fmt.Println(buildShellResumeCommand(effectiveDir, resume))
		return nil
	}
	if copyMode {
		effectiveDir := workDir
		if !workdirUsable {
			effectiveDir = ""
		}
		if err := copyToClipboard(ctx, buildShellResumeCommand(effectiveDir, resume)); err != nil {
			return fmt.Errorf("copy to clipboard: %w", err)
		}
		fmt.Fprintln(os.Stderr, "Resume command copied to clipboard.")
		return nil
	}

	// Default: spawn the agent CLI directly. We do NOT try to change
	// the parent shell's cwd — that's impossible from a child process.
	// Setting Cmd.Dir scopes the spawn; the user's shell stays put.
	// Use a fresh background context so the agent process is not bound
	// to the 15s API timeout above.
	exe := exec.Command(resume.Bin, resume.Args...)
	if workdirUsable {
		exe.Dir = workDir
	}
	exe.Stdin = os.Stdin
	exe.Stdout = os.Stdout
	exe.Stderr = os.Stderr
	return exe.Run()
}

func runIssueSearch(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("q", args[0])
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetBool("include-closed"); v {
		params.Set("include_closed", "true")
	}

	path := "/api/issues/search?" + params.Encode()

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("search issues: %w", err)
	}

	issuesRaw, _ := result["issues"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	headers := []string{"ID", "IDENTIFIER", "TITLE", "STATUS", "MATCH"}
	rows := make([][]string, 0, len(issuesRaw))
	for _, raw := range issuesRaw {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		matchInfo := strVal(issue, "match_source")
		if snippet := strVal(issue, "matched_snippet"); snippet != "" {
			if utf8.RuneCountInString(snippet) > 50 {
				runes := []rune(snippet)
				snippet = string(runes[:47]) + "..."
			}
			matchInfo += ": " + snippet
		}
		rows = append(rows, []string{
			truncateID(strVal(issue, "id")),
			strVal(issue, "identifier"),
			strVal(issue, "title"),
			strVal(issue, "status"),
			matchInfo,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// Subscriber commands
// ---------------------------------------------------------------------------

func runIssueSubscriberList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var subscribers []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+args[0]+"/subscribers", &subscribers); err != nil {
		return fmt.Errorf("list subscribers: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, subscribers)
	}

	headers := []string{"USER_TYPE", "USER_ID", "REASON", "CREATED"}
	rows := make([][]string, 0, len(subscribers))
	for _, s := range subscribers {
		created := strVal(s, "created_at")
		if len(created) >= 16 {
			created = created[:16]
		}
		rows = append(rows, []string{
			strVal(s, "user_type"),
			truncateID(strVal(s, "user_id")),
			strVal(s, "reason"),
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runIssueSubscriberAdd(cmd *cobra.Command, args []string) error {
	return runIssueSubscriberMutation(cmd, args[0], "subscribe")
}

func runIssueSubscriberRemove(cmd *cobra.Command, args []string) error {
	return runIssueSubscriberMutation(cmd, args[0], "unsubscribe")
}

// runIssueSubscriberMutation shares subscribe/unsubscribe logic — both endpoints
// take the same request body and only differ in the path.
func runIssueSubscriberMutation(cmd *cobra.Command, issueID, action string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{}
	userName, _ := cmd.Flags().GetString("user")
	uType, uID, hasUser, resolveErr := pickAssigneeFromFlags(ctx, client, cmd, "user", "user-id")
	if resolveErr != nil {
		return fmt.Errorf("resolve user: %w", resolveErr)
	}
	if hasUser {
		body["user_type"] = uType
		body["user_id"] = uID
	}

	var result map[string]any
	path := "/api/issues/" + issueID + "/" + action
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("%s issue: %w", action, err)
	}

	target := "caller"
	if userName != "" {
		target = userName
	} else if hasUser {
		target = truncateID(uID)
	}
	if action == "subscribe" {
		fmt.Fprintf(os.Stderr, "Subscribed %s to issue %s.\n", target, truncateID(issueID))
	} else {
		fmt.Fprintf(os.Stderr, "Unsubscribed %s from issue %s.\n", target, truncateID(issueID))
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
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

	input := strings.TrimSpace(name)
	if input == "" {
		return "", "", fmt.Errorf("no member or agent found matching %q", name)
	}
	inputLower := strings.ToLower(input)

	// Matches are collected into three priority buckets. Higher-priority buckets
	// short-circuit lower-priority matching so that, e.g., an exact name match
	// always wins over a substring collision with another candidate.
	//   1. idMatches        — full UUID or 8-char ShortID (as shown by `truncateID`).
	//   2. exactMatches     — case-insensitive full name equality.
	//   3. substringMatches — preserves the existing partial-name UX.
	var idMatches, exactMatches, substringMatches []assigneeMatch
	var errs []error

	classify := func(entityType, id, displayName string) {
		match := assigneeMatch{Type: entityType, ID: id, Name: displayName}
		if id != "" && (strings.EqualFold(id, input) || strings.EqualFold(truncateID(id), input)) {
			idMatches = append(idMatches, match)
			return
		}
		if strings.EqualFold(displayName, input) {
			exactMatches = append(exactMatches, match)
			return
		}
		if strings.Contains(strings.ToLower(displayName), inputLower) {
			substringMatches = append(substringMatches, match)
		}
	}

	// Search members.
	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+client.WorkspaceID+"/members", &members); err != nil {
		errs = append(errs, fmt.Errorf("fetch members: %w", err))
	} else {
		for _, m := range members {
			classify("member", strVal(m, "user_id"), strVal(m, "name"))
		}
	}

	// Search agents.
	var agents []map[string]any
	agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
	if err := client.GetJSON(ctx, agentPath, &agents); err != nil {
		errs = append(errs, fmt.Errorf("fetch agents: %w", err))
	} else {
		for _, a := range agents {
			classify("agent", strVal(a, "id"), strVal(a, "name"))
		}
	}

	// If both fetches failed, report the errors instead of a misleading "not found".
	if len(errs) == 2 {
		return "", "", fmt.Errorf("failed to resolve assignee: %v; %v", errs[0], errs[1])
	}

	for _, bucket := range [][]assigneeMatch{idMatches, exactMatches, substringMatches} {
		switch len(bucket) {
		case 0:
			continue
		case 1:
			return bucket[0].Type, bucket[0].ID, nil
		default:
			return "", "", ambiguousAssigneeError(input, bucket)
		}
	}
	return "", "", fmt.Errorf("no member or agent found matching %q", input)
}

func ambiguousAssigneeError(input string, matches []assigneeMatch) error {
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		parts = append(parts, fmt.Sprintf("  %s %q (%s)", m.Type, m.Name, truncateID(m.ID)))
	}
	return fmt.Errorf("ambiguous assignee %q; matches:\n%s", input, strings.Join(parts, "\n"))
}

// resolveAssigneeByID strictly resolves a canonical UUID to (assignee_type,
// assignee_id) by looking it up against the workspace's members and agents.
// It is the deterministic counterpart to resolveAssignee: callers that already
// hold a UUID (e.g. agents reading IDs from `multica workspace members
// --output json`) should use this instead of round-tripping through name
// matching, which can be ambiguous in workspaces with overlapping names.
func resolveAssigneeByID(ctx context.Context, client *cli.APIClient, id string) (string, string, error) {
	if client.WorkspaceID == "" {
		return "", "", fmt.Errorf("workspace ID is required to resolve assignees; use --workspace-id or set MULTICA_WORKSPACE_ID")
	}
	input := strings.TrimSpace(id)
	if !uuidRegexp.MatchString(input) {
		return "", "", fmt.Errorf("expected a canonical UUID, got %q", id)
	}

	var members []map[string]any
	memberErr := client.GetJSON(ctx, "/api/workspaces/"+client.WorkspaceID+"/members", &members)

	var agents []map[string]any
	agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
	agentErr := client.GetJSON(ctx, agentPath, &agents)

	if memberErr != nil && agentErr != nil {
		return "", "", fmt.Errorf("failed to resolve assignee: %v; %v", memberErr, agentErr)
	}

	for _, m := range members {
		if strings.EqualFold(strVal(m, "user_id"), input) {
			return "member", strVal(m, "user_id"), nil
		}
	}
	for _, a := range agents {
		if strings.EqualFold(strVal(a, "id"), input) {
			return "agent", strVal(a, "id"), nil
		}
	}

	return "", "", fmt.Errorf("no member or agent found with ID %q", input)
}

// pickAssigneeFromFlags reads a (name-flag, id-flag) pair off cmd and resolves
// it to (assignee_type, assignee_id). The third return reports whether either
// flag was *explicitly set*; callers use it to decide whether to write
// `assignee_*` into the request body. The two flags are mutually exclusive —
// passing both is rejected up-front so a script that accidentally sets both
// never silently applies one over the other.
//
// Presence is detected via Flags().Changed (not value-emptiness): a script
// that interpolates an empty env var (`--assignee-id "$MAYBE_UUID"`) must
// fail loudly through resolveAssignee/resolveAssigneeByID rather than silently
// degrade to "no filter / unassigned / subscribe caller", which would defeat
// the strict-UUID guarantee the new flags exist for.
func pickAssigneeFromFlags(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, nameFlag, idFlag string) (string, string, bool, error) {
	nameSet := cmd.Flags().Changed(nameFlag)
	idSet := cmd.Flags().Changed(idFlag)
	if nameSet && idSet {
		return "", "", false, fmt.Errorf("--%s and --%s are mutually exclusive", nameFlag, idFlag)
	}
	if idSet {
		idVal, _ := cmd.Flags().GetString(idFlag)
		t, i, err := resolveAssigneeByID(ctx, client, idVal)
		if err != nil {
			return "", "", true, err
		}
		return t, i, true, nil
	}
	if nameSet {
		name, _ := cmd.Flags().GetString(nameFlag)
		t, i, err := resolveAssignee(ctx, client, name)
		if err != nil {
			return "", "", true, err
		}
		return t, i, true, nil
	}
	return "", "", false, nil
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
