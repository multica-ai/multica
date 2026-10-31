package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Inbox is intentionally a thin CLI over the same endpoints used by the web
// Inbox. Filtering stays client-side because the active endpoint is bounded
// and this keeps older servers compatible with the CLI.
type inboxItem struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	RecipientType   string          `json:"recipient_type"`
	RecipientID     string          `json:"recipient_id"`
	Type            string          `json:"type"`
	Severity        string          `json:"severity"`
	IssueID         *string         `json:"issue_id"`
	IssueIdentifier string          `json:"issue_identifier,omitempty"`
	Title           string          `json:"title"`
	Body            *string         `json:"body"`
	Read            bool            `json:"read"`
	Archived        bool            `json:"archived"`
	CreatedAt       string          `json:"created_at"`
	IssueStatus     *string         `json:"issue_status"`
	IssuePriority   *string         `json:"issue_priority"`
	ProjectID       string          `json:"project_id,omitempty"`
	AssigneeType    string          `json:"assignee_type,omitempty"`
	AssigneeID      string          `json:"assignee_id,omitempty"`
	ActorType       *string         `json:"actor_type"`
	ActorID         *string         `json:"actor_id"`
	Details         json.RawMessage `json:"details"`
}

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "List and triage Inbox notifications",
}

var inboxListCmd = newInboxListCommand("list", "List Inbox notifications", runInboxList)
var inboxExportCmd = newInboxListCommand("export", "Export Inbox notifications as JSON", runInboxExport)

var inboxCountCmd = &cobra.Command{
	Use:   "count",
	Short: "Count unread Inbox notifications",
	Args:  cobra.NoArgs,
	RunE:  runInboxCount,
}

var inboxMarkReadCmd = &cobra.Command{
	Use:   "mark-read <item-id>",
	Short: "Mark one Inbox notification as read",
	Args:  exactArgs(1),
	RunE:  runInboxMarkRead,
}

var inboxDismissCmd = &cobra.Command{
	Use:   "dismiss <item-id>",
	Short: "Dismiss (archive) one Inbox notification",
	Args:  exactArgs(1),
	RunE:  runInboxDismiss,
}

var inboxBulkDismissCmd = newInboxListCommand("bulk-dismiss", "Dismiss a filtered set of Inbox notifications", runInboxBulkDismiss)

func init() {
	inboxCmd.AddCommand(inboxListCmd, inboxExportCmd, inboxCountCmd, inboxMarkReadCmd, inboxDismissCmd, inboxBulkDismissCmd)
	for _, cmd := range []*cobra.Command{inboxCountCmd, inboxMarkReadCmd, inboxDismissCmd} {
		cmd.Flags().String("workspace", "", "Workspace ID (overrides --workspace-id for this command)")
		cmd.Flags().String("output", "json", "Output format: table or json")
	}
	inboxBulkDismissCmd.Flags().Bool("dry-run", false, "Show the matched items without dismissing them")
}

func newInboxListCommand(use, short string, run func(*cobra.Command, []string) error) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: run}
	cmd.Flags().String("workspace", "", "Workspace ID (overrides --workspace-id for this command)")
	cmd.Flags().String("output", "table", "Output format: table or json")
	cmd.Flags().Bool("archived", false, "List archived notifications")
	cmd.Flags().Bool("unread", false, "Only unread notifications")
	cmd.Flags().Bool("read", false, "Only read notifications")
	cmd.Flags().String("type", "", "Notification type(s), comma-separated")
	cmd.Flags().String("severity", "", "Severity value(s), comma-separated")
	cmd.Flags().String("issue", "", "Issue UUID or identifier")
	cmd.Flags().String("actor", "", "Actor UUID or type")
	cmd.Flags().String("status", "", "Issue status value(s), comma-separated")
	cmd.Flags().String("priority", "", "Issue priority value(s), comma-separated")
	cmd.Flags().String("project", "", "Project UUID")
	cmd.Flags().String("assignee", "", "Assignee UUID or type")
	cmd.Flags().String("created-after", "", "Only items at or after this RFC3339 timestamp")
	cmd.Flags().String("created-before", "", "Only items before this RFC3339 timestamp")
	cmd.Flags().Int("limit", 100, "Maximum matching items to return")
	return cmd
}

func inboxClient(cmd *cobra.Command) (*cli.APIClient, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	if workspace, _ := cmd.Flags().GetString("workspace"); strings.TrimSpace(workspace) != "" {
		client.WorkspaceID = strings.TrimSpace(workspace)
	}
	if client.WorkspaceID == "" {
		return nil, fmt.Errorf("workspace_id is required: use --workspace, --workspace-id, or MULTICA_WORKSPACE_ID")
	}
	return client, nil
}

func fetchInboxItems(cmd *cobra.Command) (*cli.APIClient, []inboxItem, error) {
	client, err := inboxClient(cmd)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	path := "/api/inbox"
	if archived, _ := cmd.Flags().GetBool("archived"); archived {
		path = "/api/inbox/archived"
	}
	var items []inboxItem
	if err := client.GetJSON(ctx, path, &items); err != nil {
		return nil, nil, fmt.Errorf("list inbox: %w", err)
	}
	enrichInboxIssues(ctx, client, items)
	items, err = filterInboxItems(cmd, items)
	if err != nil {
		return nil, nil, err
	}
	return client, items, nil
}

// enrichInboxIssues fills the issue fields the Inbox list endpoint does not
// project. Enrichment is best-effort: an item remains useful if its issue was
// deleted or the caller can see the notification but not the issue.
func enrichInboxIssues(ctx context.Context, client *cli.APIClient, items []inboxItem) {
	issueIDs := make(map[string]struct{})
	for _, item := range items {
		if item.IssueID != nil && *item.IssueID != "" {
			issueIDs[*item.IssueID] = struct{}{}
		}
	}

	// The active Inbox is bounded, but sequential enrichment can still turn a
	// list into dozens of round trips. Use a small fixed fan-out so the command
	// remains responsive without stampeding the issue endpoint.
	cache := make(map[string]map[string]any, len(issueIDs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for issueID := range issueIDs {
		issueID := issueID
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			issue := map[string]any{}
			if err := client.GetJSON(ctx, "/api/issues/"+issueID, &issue); err != nil {
				issue = nil
			}
			mu.Lock()
			cache[issueID] = issue
			mu.Unlock()
		}()
	}
	wg.Wait()

	for i := range items {
		if items[i].IssueID == nil || *items[i].IssueID == "" {
			continue
		}
		issue := cache[*items[i].IssueID]
		if issue == nil {
			continue
		}
		items[i].IssueIdentifier = strVal(issue, "identifier")
		items[i].ProjectID = strVal(issue, "project_id")
		items[i].AssigneeType = strVal(issue, "assignee_type")
		items[i].AssigneeID = strVal(issue, "assignee_id")
		if items[i].IssueStatus == nil {
			if value := strVal(issue, "status"); value != "" {
				items[i].IssueStatus = &value
			}
		}
		if items[i].IssuePriority == nil {
			if value := strVal(issue, "priority"); value != "" {
				items[i].IssuePriority = &value
			}
		}
	}
}

func filterInboxItems(cmd *cobra.Command, items []inboxItem) ([]inboxItem, error) {
	if unread, _ := cmd.Flags().GetBool("unread"); unread {
		if read, _ := cmd.Flags().GetBool("read"); read {
			return nil, fmt.Errorf("--unread and --read are mutually exclusive")
		}
	}
	after, err := inboxTimeFlag(cmd, "created-after")
	if err != nil {
		return nil, err
	}
	before, err := inboxTimeFlag(cmd, "created-before")
	if err != nil {
		return nil, err
	}
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 0 {
		return nil, fmt.Errorf("--limit must be non-negative")
	}

	var filtered []inboxItem
	for _, item := range items {
		if !inboxItemMatches(cmd, item, after, before) {
			continue
		}
		filtered = append(filtered, item)
		if limit > 0 && len(filtered) == limit {
			break
		}
	}
	return filtered, nil
}

func inboxTimeFlag(cmd *cobra.Command, name string) (*time.Time, error) {
	value, _ := cmd.Flags().GetString(name)
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, fmt.Errorf("--%s must be RFC3339: %w", name, err)
	}
	return &parsed, nil
}

func inboxItemMatches(cmd *cobra.Command, item inboxItem, after, before *time.Time) bool {
	unread, _ := cmd.Flags().GetBool("unread")
	read, _ := cmd.Flags().GetBool("read")
	if unread && item.Read || read && !item.Read {
		return false
	}
	if !inboxCSVMatches(cmd, "type", item.Type) || !inboxCSVMatches(cmd, "severity", item.Severity) {
		return false
	}
	if !inboxCSVMatches(cmd, "status", ptrString(item.IssueStatus)) || !inboxCSVMatches(cmd, "priority", ptrString(item.IssuePriority)) {
		return false
	}
	if !inboxTextMatches(cmd, "issue", ptrString(item.IssueID), item.IssueIdentifier) {
		return false
	}
	if !inboxTextMatches(cmd, "actor", ptrString(item.ActorID), ptrString(item.ActorType)) {
		return false
	}
	if !inboxTextMatches(cmd, "project", item.ProjectID) || !inboxTextMatches(cmd, "assignee", item.AssigneeID, item.AssigneeType) {
		return false
	}
	if after != nil || before != nil {
		created, err := time.Parse(time.RFC3339, item.CreatedAt)
		if err != nil || after != nil && created.Before(*after) || before != nil && !created.Before(*before) {
			return false
		}
	}
	return true
}

func inboxCSVMatches(cmd *cobra.Command, flag, actual string) bool {
	wanted, _ := cmd.Flags().GetString(flag)
	if strings.TrimSpace(wanted) == "" {
		return true
	}
	for _, value := range strings.Split(wanted, ",") {
		if strings.EqualFold(strings.TrimSpace(value), actual) {
			return true
		}
	}
	return false
}

func inboxTextMatches(cmd *cobra.Command, flag string, actual ...string) bool {
	wanted, _ := cmd.Flags().GetString(flag)
	if strings.TrimSpace(wanted) == "" {
		return true
	}
	for _, value := range actual {
		if strings.EqualFold(strings.TrimSpace(wanted), value) {
			return true
		}
	}
	return false
}

func ptrString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func runInboxList(cmd *cobra.Command, _ []string) error {
	_, items, err := fetchInboxItems(cmd)
	if err != nil {
		return err
	}
	return printInboxItems(cmd, items)
}

func runInboxExport(cmd *cobra.Command, _ []string) error {
	_, items, err := fetchInboxItems(cmd)
	if err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, items)
}

func printInboxItems(cmd *cobra.Command, items []inboxItem) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, items)
	}
	if output != "table" {
		return fmt.Errorf("--output must be table or json")
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		issue := item.IssueIdentifier
		if issue == "" {
			issue = ptrString(item.IssueID)
		}
		rows = append(rows, []string{item.ID, issue, item.Type, item.Severity, ptrString(item.IssueStatus), fmt.Sprint(!item.Read), item.CreatedAt, item.Title})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "ISSUE", "TYPE", "SEVERITY", "STATUS", "UNREAD", "CREATED", "TITLE"}, rows)
	return nil
}

func runInboxCount(cmd *cobra.Command, _ []string) error {
	client, err := inboxClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/inbox/unread-count", &result); err != nil {
		return fmt.Errorf("count inbox: %w", err)
	}
	return printInboxResult(cmd, result, []string{"COUNT"}, []string{numVal(result, "count")})
}

func runInboxMarkRead(cmd *cobra.Command, args []string) error {
	return mutateInboxItem(cmd, args[0], "read", "mark inbox item read")
}

func runInboxDismiss(cmd *cobra.Command, args []string) error {
	return mutateInboxItem(cmd, args[0], "archive", "dismiss inbox item")
}

func mutateInboxItem(cmd *cobra.Command, itemID, action, label string) error {
	client, err := inboxClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/inbox/"+itemID+"/"+action, map[string]any{}, &result); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return printInboxResult(cmd, result, []string{"ID", "READ", "ARCHIVED"}, []string{strVal(result, "id"), fmt.Sprint(result["read"]), fmt.Sprint(result["archived"])})
}

func printInboxResult(cmd *cobra.Command, result any, headers, row []string) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	if output != "table" {
		return fmt.Errorf("--output must be table or json")
	}
	cli.PrintTable(os.Stdout, headers, [][]string{row})
	return nil
}

func runInboxBulkDismiss(cmd *cobra.Command, _ []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	if !cmd.Flags().Changed("limit") || limit <= 0 {
		return fmt.Errorf("bulk-dismiss requires an explicit positive --limit")
	}
	if !hasInboxBulkFilter(cmd) {
		return fmt.Errorf("bulk-dismiss requires at least one explicit filter")
	}
	client, items, err := fetchInboxItems(cmd)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	result := map[string]any{
		"dry_run":       dryRun,
		"matched_count": len(items),
		"matched_items": items,
	}
	if dryRun || len(items) == 0 {
		return cli.PrintJSON(os.Stdout, result)
	}

	// Archive is issue-group scoped on the server. One request per issue (or
	// per issue-less item) avoids redundant writes while preserving that API
	// contract.
	targets := make(map[string]string)
	for _, item := range items {
		key := item.ID
		if item.IssueID != nil && *item.IssueID != "" {
			key = "issue:" + *item.IssueID
		}
		targets[key] = item.ID
	}
	keys := make([]string, 0, len(targets))
	for key := range targets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	dismissed := make([]string, 0, len(keys))
	for _, key := range keys {
		itemID := targets[key]
		if err := client.PostJSON(ctx, "/api/inbox/"+itemID+"/archive", map[string]any{}, nil); err != nil {
			return fmt.Errorf("bulk-dismiss stopped after %d group(s): dismiss %s: %w", len(dismissed), itemID, err)
		}
		dismissed = append(dismissed, itemID)
	}
	result["dismissed_group_count"] = len(dismissed)
	result["dismissed_item_ids"] = dismissed
	return cli.PrintJSON(os.Stdout, result)
}

func hasInboxBulkFilter(cmd *cobra.Command) bool {
	for _, name := range []string{"archived", "unread", "read", "type", "severity", "issue", "actor", "status", "priority", "project", "assignee", "created-after", "created-before"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}
