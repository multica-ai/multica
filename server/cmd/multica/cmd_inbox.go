package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

const inboxAccessNote = "Agent tasks may read and change the runtime owner's inbox only when that owner is the task's verified upstream human originator; cloud-node credentials are refused."

var inboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "Read and manage your inbox notifications",
	Long: "Read and manage your inbox notifications: list them, mark them read or unread, archive, and snooze.\n\n" +
		"Reading or archiving a notification on an issue applies to every notification on that issue, as in the app. " +
		"Notifications cannot be deleted from the CLI.\n\n" + inboxAccessNote,
}

var inboxListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your notifications without changing read status",
	Long: "List your active notifications without changing read status. --archived lists archived notifications and --snoozed the issues you snoozed instead.\n\n" +
		"With --archived every page of the archive is read, so --issue finds old issues too. Each row carries its issue key (e.g. MUL-123), who it is from, and a short summary of the comment or change; the table cuts the summary short, JSON has all of it. Issue keys and names are looked up in extra requests and stay empty when that lookup fails.\n\n" +
		inboxAccessNote,
	Args: exactArgs(0),
	RunE: runInboxList,
}

var inboxCountCmd = &cobra.Command{
	Use:   "count",
	Short: "Show your unread notification count",
	Long:  "Show the unread notification count in the current workspace and in every workspace you belong to. Snoozed issues are not counted. In an agent run, only the workspace the run belongs to is listed; the owner's other workspaces are left out.\n\n" + inboxAccessNote,
	Args:  exactArgs(0),
	RunE:  runInboxCount,
}

var inboxReadCmd = &cobra.Command{
	Use:   "read [<id>...]",
	Short: "Mark notifications read",
	Long: "Mark notifications read: the given notification ids, every notification on one issue with --issue, or the whole inbox with --all --yes.\n\n" +
		"Reading a notification on an issue reads every unread notification on that issue, as opening it in the app does. Snoozed issues are left unread by --issue and --all.\n\n" +
		inboxAccessNote,
	RunE: runInboxRead,
}

var inboxUnreadCmd = &cobra.Command{
	Use:   "unread <id>...",
	Short: "Mark notifications unread",
	Long:  "Mark notifications unread, to come back to them later. Only the given notification changes, not the rest of its issue.\n\n" + inboxAccessNote,
	Args:  minimumArgs(1),
	RunE:  runInboxUnread,
}

var inboxArchiveCmd = &cobra.Command{
	Use:   "archive [<id>...]",
	Short: "Archive notifications",
	Long: "Archive notifications: the given notification ids, or in bulk with --all, --read (issues whose newest notification is read) or --completed (issues in a done or closed status). Bulk archiving needs --yes. --all and --read leave snoozed issues alone; --completed also archives snoozed issues that are done or closed, ending the snooze, as in the app.\n\n" +
		"Archiving a notification on an issue archives every notification on that issue. Archiving does not change the issue itself; `multica inbox unarchive` brings notifications back.\n\n" +
		inboxAccessNote,
	RunE: runInboxArchive,
}

var inboxUnarchiveCmd = &cobra.Command{
	Use:   "unarchive <id>...",
	Short: "Restore archived notifications",
	Long:  "Restore archived notifications to the inbox, with every notification on the same issue, in the read state they were archived in.\n\n" + inboxAccessNote,
	Args:  minimumArgs(1),
	RunE:  runInboxUnarchive,
}

var inboxSnoozeCmd = &cobra.Command{
	Use:   "snooze <id>... --until <time>",
	Short: "Snooze the issues of notifications until a time",
	Long: "Take the issue each notification is on out of the inbox until a time, as Snooze does in the app. Snoozing a snoozed issue changes its time.\n\n" +
		"--until takes an RFC 3339 time, a duration from now (30m, 3h, 1d, 1w), or \"tomorrow\" (9:00 local time). Like the app, a snooze of a day or more made between 22:00 and 6:00 comes back at 9:00. The time must be within a year.\n\n" +
		inboxAccessNote,
	Args: minimumArgs(1),
	RunE: runInboxSnooze,
}

var inboxUnsnoozeCmd = &cobra.Command{
	Use:   "unsnooze <id>...",
	Short: "End the snooze on the issues of notifications",
	Long:  "Bring the issue each notification is on back into the inbox now, unread.\n\n" + inboxAccessNote,
	Args:  minimumArgs(1),
	RunE:  runInboxUnsnooze,
}

func init() {
	inboxCmd.AddCommand(inboxListCmd, inboxCountCmd, inboxReadCmd, inboxUnreadCmd, inboxArchiveCmd, inboxUnarchiveCmd, inboxSnoozeCmd, inboxUnsnoozeCmd)
	registerInboxListFlags(inboxListCmd)
	registerInboxReadFlags(inboxReadCmd)
	registerInboxArchiveFlags(inboxArchiveCmd)
	registerInboxSnoozeFlags(inboxSnoozeCmd)
	for _, cmd := range []*cobra.Command{inboxCountCmd, inboxUnreadCmd, inboxUnarchiveCmd, inboxUnsnoozeCmd} {
		registerInboxOutputFlag(cmd)
	}
}

func registerInboxReadFlags(cmd *cobra.Command) {
	cmd.Flags().String("issue", "", "Mark every notification on this issue read (issue key or UUID)")
	cmd.Flags().Bool("all", false, "Mark the whole inbox read (needs --yes)")
	cmd.Flags().BoolP("yes", "y", false, "Confirm a bulk change without a prompt")
	registerInboxOutputFlag(cmd)
}

func registerInboxArchiveFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("all", false, "Archive the whole inbox (needs --yes)")
	cmd.Flags().Bool("read", false, "Archive every issue whose newest notification is read (needs --yes)")
	cmd.Flags().Bool("completed", false, "Archive every issue in a done or closed status (needs --yes)")
	cmd.Flags().BoolP("yes", "y", false, "Confirm a bulk change without a prompt")
	registerInboxOutputFlag(cmd)
}

func registerInboxSnoozeFlags(cmd *cobra.Command) {
	cmd.Flags().String("until", "", "When the issue comes back: RFC 3339 time, 30m, 3h, 1d, 1w, or tomorrow")
	registerInboxOutputFlag(cmd)
}

func registerInboxListFlags(cmd *cobra.Command) {
	cmd.Flags().String("project", "", "Filter by issue project ID or unique ID prefix")
	cmd.Flags().String("type", "", "Filter by notification type (e.g. mentioned)")
	cmd.Flags().Bool("unread", false, "Only unread notifications")
	cmd.Flags().String("issue", "", "Only notifications on this issue (issue key or UUID)")
	cmd.Flags().String("since", "", "Only notifications received since a time: RFC 3339, or a duration back from now (2h, 1d, 1w)")
	cmd.Flags().Bool("archived", false, "List archived notifications instead of active ones")
	cmd.Flags().Bool("snoozed", false, "List snoozed issues instead of active notifications")
	registerInboxOutputFlag(cmd)
}

func registerInboxOutputFlag(cmd *cobra.Command) {
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func inboxOutput(cmd *cobra.Command) (string, error) {
	output, _ := cmd.Flags().GetString("output")
	if output != "json" && output != "table" {
		return "", fmt.Errorf("output must be table or json")
	}
	return output, nil
}

type inboxListItem struct {
	ID              string  `json:"id"`
	Type            string  `json:"type"`
	Title           string  `json:"title"`
	IssueID         *string `json:"issue_id"`
	IssueIdentifier string  `json:"issue_identifier,omitempty"`
	CommentID       *string `json:"comment_id,omitempty"`
	IssueProjectID  *string `json:"issue_project_id"`
	ActorType       *string `json:"actor_type,omitempty"`
	ActorID         *string `json:"actor_id,omitempty"`
	Actor           string  `json:"actor,omitempty"`
	Summary         string  `json:"summary,omitempty"`
	Read            bool    `json:"read"`
	CreatedAt       string  `json:"created_at"`
}

type inboxSnoozedItem struct {
	ID              string `json:"id"`
	IssueID         string `json:"issue_id"`
	IssueIdentifier string `json:"issue_identifier,omitempty"`
	Title           string `json:"title,omitempty"`
	SnoozedUntil    string `json:"snoozed_until"`
	SnoozedAt       string `json:"snoozed_at"`
	Updates         int64  `json:"updates"`
}

func runInboxList(cmd *cobra.Command, _ []string) error {
	output, err := inboxOutput(cmd)
	if err != nil {
		return err
	}
	archived, _ := cmd.Flags().GetBool("archived")
	snoozed, _ := cmd.Flags().GetBool("snoozed")
	if archived && snoozed {
		return fmt.Errorf("--archived and --snoozed cannot be combined")
	}
	if snoozed {
		for _, name := range []string{"project", "type", "unread", "since"} {
			if cmd.Flags().Changed(name) {
				return fmt.Errorf("--%s does not apply to --snoozed", name)
			}
		}
	}
	var since time.Time
	if v, _ := cmd.Flags().GetString("since"); v != "" {
		since, err = parseInboxTime(v, time.Now(), true)
		if err != nil {
			return fmt.Errorf("--since: %w", err)
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()

	projectID := ""
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		project, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return err
		}
		projectID = project.ID
	}
	issueID := ""
	if v, _ := cmd.Flags().GetString("issue"); v != "" {
		issue, err := resolveIssueRef(ctx, client, v)
		if err != nil {
			return err
		}
		issueID = issue.ID
	}

	if snoozed {
		return listSnoozedInbox(ctx, cmd.Context(), client, issueID, output)
	}

	unreadOnly, _ := cmd.Flags().GetBool("unread")
	notifType, _ := cmd.Flags().GetString("type")
	var response []inboxListRow
	if archived {
		// The archive lists one notification per issue, so the type has to
		// be matched before that one is picked, not after.
		filters := url.Values{}
		if notifType != "" {
			filters.Set("notification_types", notifType)
		}
		if issueID != "" {
			filters.Set("group_id", issueID)
		}
		if projectID != "" {
			filters.Set("projects", projectID)
		}
		if unreadOnly {
			filters.Set("unread_only", "true")
		}
		if !since.IsZero() {
			filters.Set("received_after", since.UTC().Format(time.RFC3339Nano))
		}
		response, err = listArchivedInbox(cmd.Context(), client, filters)
	} else {
		err = client.GetJSON(ctx, "/api/inbox", &response)
	}
	if err != nil {
		return fmt.Errorf("list inbox: %w", err)
	}
	items := make([]inboxListItem, 0, len(response))
	for _, row := range response {
		if projectID != "" && (row.IssueProjectID == nil || !strings.EqualFold(*row.IssueProjectID, projectID)) {
			continue
		}
		if notifType != "" && row.Type != notifType {
			continue
		}
		if unreadOnly && row.Read {
			continue
		}
		if issueID != "" && (row.IssueID == nil || !strings.EqualFold(*row.IssueID, issueID)) {
			continue
		}
		if !since.IsZero() {
			created, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
			if err != nil || created.Before(since) {
				continue
			}
		}
		var details struct {
			CommentID *string `json:"comment_id"`
		}
		if json.Unmarshal(row.Details, &details) == nil {
			row.CommentID = details.CommentID
		}
		if row.Body != nil {
			row.Summary = strings.Join(strings.Fields(*row.Body), " ")
		}
		items = append(items, row.inboxListItem)
	}

	issueIDs := make([]string, 0, len(items))
	for _, item := range items {
		if item.IssueID != nil {
			issueIDs = append(issueIDs, *item.IssueID)
		}
	}
	issues := lookupInboxIssues(cmd.Context(), client, issueIDs)
	actorCtx, actorCancel := cli.APIContext(cmd.Context())
	defer actorCancel()
	actors := loadActorDisplayLookup(actorCtx, client)
	for i := range items {
		if items[i].IssueID != nil {
			items[i].IssueIdentifier = issues[strings.ToLower(*items[i].IssueID)].Identifier
		}
		if items[i].ActorType != nil && items[i].ActorID != nil {
			items[i].Actor = actors.actor(*items[i].ActorType, *items[i].ActorID)
		}
	}

	if output == "json" {
		return cli.PrintJSON(os.Stdout, items)
	}
	rows := make([][]string, 0, len(items))
	value := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	for _, item := range items {
		rows = append(rows, []string{item.ID, item.Type, oneLine(item.Title, 0), value(item.IssueID), value(item.CommentID), value(item.IssueProjectID), strconv.FormatBool(item.Read), item.CreatedAt,
			item.IssueIdentifier, item.Actor, oneLine(item.Summary, 80)})
	}
	// New columns go after the original ones so their positions stay put.
	cli.PrintTable(os.Stdout, []string{"ID", "TYPE", "TITLE", "ISSUE", "COMMENT", "PROJECT", "READ", "CREATED", "KEY", "FROM", "SUMMARY"}, rows)
	return nil
}

type inboxListRow struct {
	inboxListItem
	Body    *string         `json:"body"`
	Details json.RawMessage `json:"details"`
}

// listArchivedInbox reads every page of the archive with the given filters,
// each page under its own deadline.
func listArchivedInbox(parent context.Context, client *cli.APIClient, filters url.Values) ([]inboxListRow, error) {
	rows := []inboxListRow{}
	cursor := ""
	for {
		params := url.Values{"limit": {"100"}}
		for key, values := range filters {
			params[key] = values
		}
		if cursor != "" {
			params.Set("cursor", cursor)
		}
		var page struct {
			Items      []inboxListRow `json:"items"`
			NextCursor *string        `json:"next_cursor"`
			HasMore    bool           `json:"has_more"`
		}
		ctx, cancel := cli.APIContext(parent)
		err := client.GetJSON(ctx, "/api/inbox/archived/page?"+params.Encode(), &page)
		cancel()
		if err != nil {
			return nil, err
		}
		rows = append(rows, page.Items...)
		if !page.HasMore || page.NextCursor == nil || *page.NextCursor == "" {
			return rows, nil
		}
		cursor = *page.NextCursor
	}
}

func listSnoozedInbox(ctx, parent context.Context, client *cli.APIClient, issueID, output string) error {
	var response []struct {
		inboxSnoozedItem
		// The server calls the notification id item_id here; the CLI calls it
		// id everywhere, so the column feeds straight into unsnooze.
		ItemID string `json:"item_id"`
	}
	if err := client.GetJSON(ctx, "/api/inbox/snoozed", &response); err != nil {
		return fmt.Errorf("list snoozed inbox: %w", err)
	}
	items := make([]inboxSnoozedItem, 0, len(response))
	ids := make([]string, 0, len(response))
	for _, row := range response {
		if issueID != "" && !strings.EqualFold(row.IssueID, issueID) {
			continue
		}
		row.ID = row.ItemID
		items = append(items, row.inboxSnoozedItem)
		ids = append(ids, row.IssueID)
	}
	issues := lookupInboxIssues(parent, client, ids)
	for i := range items {
		issue := issues[strings.ToLower(items[i].IssueID)]
		items[i].IssueIdentifier, items[i].Title = issue.Identifier, issue.Title
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, items)
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		issue := item.IssueIdentifier
		if issue == "" {
			issue = item.IssueID
		}
		rows = append(rows, []string{item.ID, issue, oneLine(item.Title, 0), item.SnoozedUntil, strconv.FormatInt(item.Updates, 10)})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "ISSUE", "TITLE", "UNTIL", "UPDATES"}, rows)
	return nil
}

type inboxIssue struct {
	Identifier string
	Title      string
}

// lookupInboxIssues fetches the key and title of each issue in one request per
// hundred ids, each under its own deadline. It is best effort: notifications
// still list without their issue key when the lookup fails.
func lookupInboxIssues(parent context.Context, client *cli.APIClient, ids []string) map[string]inboxIssue {
	const chunk = 100
	issues := map[string]inboxIssue{}
	unique := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.ToLower(id)
		if !seen[id] && uuidRegexp.MatchString(id) {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	for start := 0; start < len(unique); start += chunk {
		end := min(start+chunk, len(unique))
		params := url.Values{"ids": {strings.Join(unique[start:end], ",")}, "limit": {strconv.Itoa(chunk)}}
		var resp struct {
			Issues []struct {
				ID         string `json:"id"`
				Identifier string `json:"identifier"`
				Title      string `json:"title"`
			} `json:"issues"`
		}
		ctx, cancel := cli.APIContext(parent)
		err := client.GetJSON(ctx, "/api/issues?"+params.Encode(), &resp)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not look up issue keys: %v\n", err)
			return issues
		}
		for _, issue := range resp.Issues {
			issues[strings.ToLower(issue.ID)] = inboxIssue{Identifier: issue.Identifier, Title: issue.Title}
		}
	}
	return issues
}

// oneLine collapses whitespace and, when limit is positive, cuts to limit runes.
func oneLine(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if limit <= 0 || utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return string(runes[:limit-1]) + "…"
}

func runInboxCount(cmd *cobra.Command, _ []string) error {
	output, err := inboxOutput(cmd)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()

	var count struct {
		Count int64 `json:"count"`
	}
	if err := client.GetJSON(ctx, "/api/inbox/unread-count", &count); err != nil {
		return fmt.Errorf("count unread inbox: %w", err)
	}
	var workspaces []struct {
		WorkspaceID string `json:"workspace_id"`
		Count       int64  `json:"count"`
	}
	if err := client.GetJSON(ctx, "/api/inbox/unread-summary", &workspaces); err != nil {
		return fmt.Errorf("summarize unread inbox: %w", err)
	}
	if workspaces == nil {
		workspaces = []struct {
			WorkspaceID string `json:"workspace_id"`
			Count       int64  `json:"count"`
		}{}
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{
			"workspace_id": client.WorkspaceID,
			"unread":       count.Count,
			"workspaces":   workspaces,
		})
	}
	fmt.Fprintf(os.Stdout, "Unread in this workspace: %d\n", count.Count)
	if len(workspaces) > 0 {
		rows := make([][]string, 0, len(workspaces))
		for _, ws := range workspaces {
			rows = append(rows, []string{ws.WorkspaceID, strconv.FormatInt(ws.Count, 10)})
		}
		fmt.Fprintln(os.Stdout)
		cli.PrintTable(os.Stdout, []string{"WORKSPACE", "UNREAD"}, rows)
	}
	return nil
}

// inboxWriteResult is what every write prints. Count is the number of
// notifications the server actually changed, issue groups included; an id
// whose change was already in place counts 0.
type inboxWriteResult struct {
	Action       string              `json:"action"`
	Count        int64               `json:"count"`
	Items        []inboxWriteItem    `json:"items,omitempty"`
	Failed       []inboxWriteFailure `json:"failed,omitempty"`
	IssueID      string              `json:"issue_id,omitempty"`
	SnoozedUntil string              `json:"snoozed_until,omitempty"`
}

type inboxWriteItem struct {
	ID      string  `json:"id"`
	IssueID *string `json:"issue_id"`
	Count   int64   `json:"count"`
}

type inboxWriteFailure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// inboxItemCall changes one notification and reports what changed.
type inboxItemCall func(ctx context.Context, client *cli.APIClient, id string) (inboxWriteItem, error)

func inboxItemIDs(args []string) ([]string, error) {
	ids := make([]string, 0, len(args))
	seen := map[string]bool{}
	for _, arg := range args {
		id := strings.ToLower(strings.TrimSpace(arg))
		if !uuidRegexp.MatchString(id) {
			return nil, fmt.Errorf("notification id %q is not a full UUID; copy it from `multica inbox list --output json`", arg)
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids, nil
}

// runInboxItemWrites applies one change to each notification in turn. Every
// id is validated before the first request, and a failure on one id does not
// stop the rest; the command exits non-zero when any failed.
func runInboxItemWrites(cmd *cobra.Command, args []string, result inboxWriteResult, past string, call inboxItemCall) error {
	output, err := inboxOutput(cmd)
	if err != nil {
		return err
	}
	ids, err := inboxItemIDs(args)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	for _, id := range ids {
		ctx, cancel := cli.APIContext(cmd.Context())
		item, err := call(ctx, client, id)
		cancel()
		if err != nil {
			msg := cli.ServerMessage(err)
			if msg == "" {
				msg = err.Error()
			}
			result.Failed = append(result.Failed, inboxWriteFailure{ID: id, Error: msg})
			continue
		}
		item.ID = id
		result.Items = append(result.Items, item)
		result.Count += item.Count
	}
	if output == "json" {
		if err := cli.PrintJSON(os.Stdout, result); err != nil {
			return err
		}
	} else {
		for _, item := range result.Items {
			fmt.Fprintf(os.Stdout, "%s: %s\n", item.ID, inboxCountText(item.Count))
		}
		for _, failure := range result.Failed {
			fmt.Fprintf(os.Stderr, "Failed %s: %s\n", failure.ID, failure.Error)
		}
		if err := printInboxCount(output, result, past); err != nil {
			return err
		}
	}
	if len(result.Failed) > 0 {
		return fmt.Errorf("%d of %d notifications could not be %s", len(result.Failed), len(ids), past)
	}
	return nil
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// postInboxItem posts to one notification's action endpoint and returns the
// issue and change count the response names.
func postInboxItem(action string, body any) inboxItemCall {
	return func(ctx context.Context, client *cli.APIClient, id string) (inboxWriteItem, error) {
		var resp struct {
			IssueID *string `json:"issue_id"`
			Count   *int64  `json:"count"`
		}
		if err := client.PostJSON(ctx, "/api/inbox/"+url.PathEscape(id)+"/"+action, body, &resp); err != nil {
			return inboxWriteItem{}, err
		}
		// Older servers leave count out of read, unread, archive and unarchive;
		// the notification itself is the one change their answer confirms.
		count := int64(1)
		if resp.Count != nil {
			count = *resp.Count
		}
		return inboxWriteItem{IssueID: resp.IssueID, Count: count}, nil
	}
}

func inboxCountText(count int64) string {
	switch count {
	case 0:
		return "no change"
	case 1:
		return "1 notification"
	}
	return fmt.Sprintf("%d notifications", count)
}

// runInboxBatch runs one server-side bulk change after confirmation and
// prints the server's count.
func runInboxBatch(cmd *cobra.Command, action, path, prompt, past string) error {
	output, err := inboxOutput(cmd)
	if err != nil {
		return err
	}
	if confirmed, err := confirmInboxBatch(cmd, prompt); !confirmed {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(cmd.Context())
	defer cancel()
	var resp struct {
		Count int64 `json:"count"`
	}
	if err := client.PostJSON(ctx, path, nil, &resp); err != nil {
		return fmt.Errorf("%s: %w", strings.ReplaceAll(action, "_", " "), err)
	}
	return printInboxCount(output, inboxWriteResult{Action: action, Count: resp.Count}, past)
}

func printInboxCount(output string, result inboxWriteResult, past string) error {
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	noun := "notifications"
	if result.Count == 1 {
		noun = "notification"
	}
	fmt.Fprintf(os.Stdout, "%s %d %s\n", capitalize(past), result.Count, noun)
	return nil
}

// confirmInboxBatch guards the bulk changes: --yes, or a "y" typed at the
// prompt. A run that cannot answer the prompt fails without changing anything.
func confirmInboxBatch(cmd *cobra.Command, prompt string) (bool, error) {
	if yes, _ := cmd.Flags().GetBool("yes"); yes {
		return true, nil
	}
	fmt.Fprint(os.Stderr, prompt+" [y/N] ")
	answer, readErr := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "y" || answer == "yes" {
		return true, nil
	}
	if answer == "" && readErr != nil {
		return false, fmt.Errorf("no confirmation received (stdin is not interactive); re-run with --yes to confirm the bulk change")
	}
	fmt.Fprintln(os.Stderr, "Aborted.")
	return false, nil
}

// inboxMode returns the one mode the flags select, refusing a mix of ids and
// mode flags or of two mode flags.
func inboxMode(cmd *cobra.Command, args []string, flags ...string) (string, error) {
	mode := ""
	if len(args) > 0 {
		mode = "ids"
	}
	for _, name := range flags {
		if !cmd.Flags().Changed(name) {
			continue
		}
		if f := cmd.Flags().Lookup(name); f.Value.Type() == "bool" && f.Value.String() != "true" {
			continue
		}
		if mode != "" {
			return "", fmt.Errorf("pass notification ids or exactly one of --%s", strings.Join(flags, ", --"))
		}
		mode = name
	}
	if mode == "" {
		return "", fmt.Errorf("pass notification ids or one of --%s", strings.Join(flags, ", --"))
	}
	return mode, nil
}

func runInboxRead(cmd *cobra.Command, args []string) error {
	mode, err := inboxMode(cmd, args, "issue", "all")
	if err != nil {
		return err
	}
	switch mode {
	case "all":
		return runInboxBatch(cmd, "mark_all_read", "/api/inbox/mark-all-read", "Mark every notification in your inbox read?", "marked read")
	case "issue":
		output, err := inboxOutput(cmd)
		if err != nil {
			return err
		}
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(cmd.Context())
		defer cancel()
		ref, _ := cmd.Flags().GetString("issue")
		issue, err := resolveIssueRef(ctx, client, ref)
		if err != nil {
			return err
		}
		var resp struct {
			Count int64 `json:"count"`
		}
		if err := client.PostJSON(ctx, "/api/inbox/issues/"+url.PathEscape(issue.ID)+"/read", nil, &resp); err != nil {
			return fmt.Errorf("mark issue %s read: %w", issue.Display, err)
		}
		return printInboxCount(output, inboxWriteResult{Action: "read", Count: resp.Count, IssueID: issue.ID}, "marked read")
	}
	return runInboxItemWrites(cmd, args, inboxWriteResult{Action: "read"}, "marked read", postInboxItem("read", nil))
}

func runInboxUnread(cmd *cobra.Command, args []string) error {
	return runInboxItemWrites(cmd, args, inboxWriteResult{Action: "unread"}, "marked unread", postInboxItem("unread", nil))
}

func runInboxArchive(cmd *cobra.Command, args []string) error {
	mode, err := inboxMode(cmd, args, "all", "read", "completed")
	if err != nil {
		return err
	}
	switch mode {
	case "all":
		return runInboxBatch(cmd, "archive_all", "/api/inbox/archive-all", "Archive every notification in your inbox?", "archived")
	case "read":
		return runInboxBatch(cmd, "archive_read", "/api/inbox/archive-all-read", "Archive every issue in your inbox whose newest notification is read?", "archived")
	case "completed":
		return runInboxBatch(cmd, "archive_completed", "/api/inbox/archive-completed", "Archive every issue in your inbox that is done or closed?", "archived")
	}
	return runInboxItemWrites(cmd, args, inboxWriteResult{Action: "archive"}, "archived", postInboxItem("archive", nil))
}

func runInboxUnarchive(cmd *cobra.Command, args []string) error {
	return runInboxItemWrites(cmd, args, inboxWriteResult{Action: "unarchive"}, "unarchived", postInboxItem("unarchive", nil))
}

func runInboxSnooze(cmd *cobra.Command, args []string) error {
	value, _ := cmd.Flags().GetString("until")
	if value == "" {
		return fmt.Errorf("--until is required")
	}
	until, err := parseInboxTime(value, time.Now(), false)
	if err != nil {
		return fmt.Errorf("--until: %w", err)
	}
	if !until.After(time.Now()) {
		return fmt.Errorf("--until must be in the future")
	}
	formatted := until.UTC().Format(time.RFC3339)
	snooze := postInboxItem("snooze", map[string]string{"until": formatted})
	snoozedIssues := map[string]bool{}
	// Every id on an issue snoozes the whole issue to the same time, so only
	// the first one on each issue changes anything.
	call := func(ctx context.Context, client *cli.APIClient, id string) (inboxWriteItem, error) {
		item, err := snooze(ctx, client, id)
		if err == nil && item.IssueID != nil {
			key := strings.ToLower(*item.IssueID)
			if snoozedIssues[key] {
				item.Count = 0
			}
			snoozedIssues[key] = true
		}
		return item, err
	}
	return runInboxItemWrites(cmd, args, inboxWriteResult{Action: "snooze", SnoozedUntil: formatted}, "snoozed", call)
}

func runInboxUnsnooze(cmd *cobra.Command, args []string) error {
	return runInboxItemWrites(cmd, args, inboxWriteResult{Action: "unsnooze"}, "unsnoozed", postInboxItem("unsnooze", nil))
}

var inboxRelativeTime = regexp.MustCompile(`^(\d+)\s*([mhdw])$`)

// Mirrors the app's snooze presets (packages/core/inbox/snooze.ts): a day or
// more keeps the clock time, except at night, when it lands in the morning.
const (
	inboxMorningHour    = 9
	inboxNightStartHour = 22
	inboxNightEndHour   = 6
)

// parseInboxTime reads an RFC 3339 time, "tomorrow" (9:00 local), or a
// duration such as 30m, 3h, 1d or 1w. Durations count forward from now, or
// backward when back is set.
func parseInboxTime(value string, now time.Time, back bool) (time.Time, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if t, err := time.Parse(time.RFC3339, strings.ToUpper(value)); err == nil {
		return t, nil
	}
	if value == "tomorrow" && !back {
		y, m, d := now.AddDate(0, 0, 1).Date()
		return time.Date(y, m, d, inboxMorningHour, 0, 0, 0, now.Location()), nil
	}
	match := inboxRelativeTime.FindStringSubmatch(value)
	if match == nil {
		if d, err := time.ParseDuration(value); err == nil && d > 0 {
			if back {
				return now.Add(-d), nil
			}
			return now.Add(d), nil
		}
		return time.Time{}, fmt.Errorf("%q is not an RFC 3339 time, a duration such as 30m, 3h, 1d or 1w, or tomorrow", value)
	}
	n, err := strconv.Atoi(match[1])
	if err != nil || n <= 0 {
		return time.Time{}, fmt.Errorf("%q must be a positive duration", value)
	}
	sign := 1
	if back {
		sign = -1
	}
	switch match[2] {
	case "m":
		return now.Add(time.Duration(sign*n) * time.Minute), nil
	case "h":
		return now.Add(time.Duration(sign*n) * time.Hour), nil
	}
	days := n
	if match[2] == "w" {
		days = n * 7
	}
	t := now.AddDate(0, 0, sign*days)
	if hour := now.Hour(); !back && (hour >= inboxNightStartHour || hour < inboxNightEndHour) {
		y, m, d := t.Date()
		t = time.Date(y, m, d, inboxMorningHour, 0, 0, 0, now.Location())
	}
	return t, nil
}
