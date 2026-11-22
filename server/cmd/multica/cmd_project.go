package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Work with projects",
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List projects in the workspace",
	Long: "List projects in the workspace.\n\n" +
		"--status, --priority and --lead take several values, repeated or comma-separated;\n" +
		"a project matches when it has any of them. --lead takes a member or agent name\n" +
		"(fuzzy match), me, or none for projects without a lead.\n\n" +
		"Without --limit every matching project is listed. --output json prints the\n" +
		"projects as an array, as before; when --limit leaves more, a hint with the next\n" +
		"--offset goes to stderr.",
	RunE: runProjectList,
}

var projectGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get project details",
	Args:  exactArgs(1),
	RunE:  runProjectGet,
}

var projectProgressCmd = &cobra.Command{
	Use:   "progress <id>",
	Short: "Show project progress: completed vs. scope, weekly trend and forecast",
	Long: `Show project progress.

Scope is every issue in the project except cancelled ones (closed category).
Completed counts issues in the done category; started issues are tracked but
do not add to the percentage. Each issue counts as one.

--history prints the weekly trend (scope, started, completed per week).
--by assignee breaks scope and completion down per assignee; --by label does
the same per label, where an issue with several labels counts under each.
JSON output always carries the full response.`,
	Args: exactArgs(1),
	RunE: runProjectProgress,
}

var projectOverviewCmd = &cobra.Command{
	Use:   "overview <id>",
	Short: "Show everything on a project's Overview in one read",
	Long: `Show a project's Overview: the project itself (description included),
its progress with weekly history, forecast and breakdowns by assignee and
label, its attached resources, the agents running on its issues now and its
most recent activity.

The JSON shape is {"project": ..., "progress": ..., "resources": [...],
"working_agents": [...], "activity": [...]}; new Overview sections are added
as new top-level keys.`,
	Args: exactArgs(1),
	RunE: runProjectOverview,
}

var projectActivityCmd = &cobra.Command{
	Use:   "activity <id>",
	Short: "List recent activity on a project's issues, newest first",
	Long: `List recent activity on a project's issues, newest first: issues created,
and changes to status, assignee, priority, dates and title. Description edits
and run bookkeeping stay on each issue's own timeline.

When more entries exist, the cursor for the next page goes to stderr; pass it
back with --before. --output json prints {"entries": [...], "next_cursor": ...}.`,
	Args: exactArgs(1),
	RunE: runProjectActivity,
}

var projectCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new project",
	RunE:  runProjectCreate,
}

var projectUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a project",
	Args:  exactArgs(1),
	RunE:  runProjectUpdate,
}

var projectDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a project",
	Args:  exactArgs(1),
	RunE:  runProjectDelete,
}

var projectArchiveCmd = &cobra.Command{
	Use:   "archive <id>",
	Short: "Archive a project: hide it from default lists and stop new issues from being filed into it",
	Long: `Archive a project. It disappears from default project lists and pickers and
accepts no new issues; its existing issues stay readable and editable, and its
status is unchanged. Autopilots that file into it are paused. Undo with
` + "`multica project restore <id>`" + `.`,
	Args: exactArgs(1),
	RunE: runProjectArchive,
}

var projectRestoreCmd = &cobra.Command{
	Use:   "restore <id>",
	Short: "Restore an archived project (paused autopilots stay paused)",
	Args:  exactArgs(1),
	RunE:  runProjectRestore,
}

var projectStatusCmd = &cobra.Command{
	Use:   "status <id> <status>",
	Short: "Change project status",
	Args:  exactArgs(2),
	RunE:  runProjectStatus,
}

var projectResourceCmd = &cobra.Command{
	Use:   "resource",
	Short: "Manage resources attached to a project",
}

var projectResourceListCmd = &cobra.Command{
	Use:   "list <project-id>",
	Short: "List resources attached to a project",
	Args:  exactArgs(1),
	RunE:  runProjectResourceList,
}

var projectResourceAddCmd = &cobra.Command{
	Use:   "add <project-id>",
	Short: "Attach a resource to a project (e.g. --type github_repo --url <url>)",
	Args:  exactArgs(1),
	RunE:  runProjectResourceAdd,
}

var projectResourceUpdateCmd = &cobra.Command{
	Use:   "update <project-id> <resource-id>",
	Short: "Edit an attached resource (ref payload, label, or position)",
	Args:  exactArgs(2),
	RunE:  runProjectResourceUpdate,
}

var projectResourceRemoveCmd = &cobra.Command{
	Use:   "remove <project-id> <resource-id>",
	Short: "Detach a resource from a project",
	Args:  exactArgs(2),
	RunE:  runProjectResourceRemove,
}

var projectMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Manage the members of a private project",
}

var projectMemberListCmd = &cobra.Command{
	Use:   "list <project-id>",
	Short: "List a project's members",
	Args:  exactArgs(1),
	RunE:  runProjectMemberList,
}

var projectMemberAddCmd = &cobra.Command{
	Use:   "add <project-id> <member>...",
	Short: "Add workspace members to a project (name, email or user id)",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runProjectMemberAdd,
}

var projectMemberRemoveCmd = &cobra.Command{
	Use:   "remove <project-id> <member>",
	Short: "Remove a member from a project (name, email or user id)",
	Args:  exactArgs(2),
	RunE:  runProjectMemberRemove,
}

var validProjectVisibilities = []string{"workspace", "private"}

func validateProjectVisibility(v string) error {
	for _, allowed := range validProjectVisibilities {
		if v == allowed {
			return nil
		}
	}
	return fmt.Errorf("invalid visibility %q; valid values: %s", v, strings.Join(validProjectVisibilities, ", "))
}

var validProjectStatuses = []string{
	"planned", "in_progress", "paused", "completed", "cancelled",
}

// validateProjectStatus rejects unknown statuses client-side so a typo fails
// fast with the valid list instead of a server round-trip and a 400. Shared by
// `project create`, `project update`, and `project status`.
func validateProjectStatus(status string) error {
	for _, s := range validProjectStatuses {
		if s == status {
			return nil
		}
	}
	return fmt.Errorf("invalid status %q; valid values: %s", status, strings.Join(validProjectStatuses, ", "))
}

func init() {
	projectCmd.AddCommand(projectListCmd)
	projectCmd.AddCommand(projectGetCmd)
	projectCmd.AddCommand(projectProgressCmd)
	projectCmd.AddCommand(projectOverviewCmd)
	projectCmd.AddCommand(projectActivityCmd)
	projectCmd.AddCommand(projectCreateCmd)
	projectCmd.AddCommand(projectUpdateCmd)
	projectCmd.AddCommand(projectDeleteCmd)
	projectCmd.AddCommand(projectStatusCmd)
	projectCmd.AddCommand(projectArchiveCmd)
	projectCmd.AddCommand(projectRestoreCmd)
	projectCmd.AddCommand(projectResourceCmd)

	projectResourceCmd.AddCommand(projectResourceListCmd)
	projectResourceCmd.AddCommand(projectResourceAddCmd)
	projectResourceCmd.AddCommand(projectResourceUpdateCmd)
	projectResourceCmd.AddCommand(projectResourceRemoveCmd)

	projectCmd.AddCommand(projectMemberCmd)
	projectMemberCmd.AddCommand(projectMemberListCmd)
	projectMemberCmd.AddCommand(projectMemberAddCmd)
	projectMemberCmd.AddCommand(projectMemberRemoveCmd)
	projectMemberListCmd.Flags().String("output", "table", "Output format: table or json")
	projectMemberAddCmd.Flags().String("output", "table", "Output format: table or json")

	// project list
	registerProjectListFlags(projectListCmd)

	// project get
	projectGetCmd.Flags().String("output", "json", "Output format: table or json")

	projectProgressCmd.Flags().String("output", "table", "Output format: table or json")
	projectOverviewCmd.Flags().String("output", "json", "Output format: table or json")
	projectProgressCmd.Flags().Bool("history", false, "Print the weekly trend")
	projectProgressCmd.Flags().String("by", "", "Break progress down by: assignee or label")
	projectActivityCmd.Flags().String("output", "table", "Output format: table or json")
	projectActivityCmd.Flags().Int("limit", 30, "Entries per page (max 100)")
	projectActivityCmd.Flags().String("before", "", "Cursor from a previous page, to read older entries")

	// project create
	projectCreateCmd.Flags().String("title", "", "Project title (required)")
	projectCreateCmd.Flags().String("description", "", "Project description")
	projectCreateCmd.Flags().String("status", "", "Project status")
	projectCreateCmd.Flags().String("icon", "", "Project icon (emoji)")
	projectCreateCmd.Flags().String("lead", "", "Lead name (member or agent)")
	projectCreateCmd.Flags().String("start-date", "", "Start date (calendar day, YYYY-MM-DD)")
	projectCreateCmd.Flags().String("due-date", "", "Due date (calendar day, YYYY-MM-DD)")
	projectCreateCmd.Flags().StringArray("repo", nil, "Attach a github_repo resource by URL (may be repeated)")
	projectCreateCmd.Flags().String("visibility", "", "Who can see the project: workspace (default) or private (its members only)")
	projectCreateCmd.Flags().StringArray("member", nil, "Member of a private project (name, email or user id; may be repeated). You are always added")
	projectCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// project resource list
	projectResourceListCmd.Flags().String("output", "table", "Output format: table or json")
	projectResourceListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	// project resource add — generic shape: any --type with a JSON --ref
	// payload works without further CLI changes. github_repo is supported via
	// dedicated shortcuts; for that type, a non-JSON --ref value is treated as
	// the default checkout ref.
	projectResourceAddCmd.Flags().String("type", "github_repo", "Resource type (e.g. github_repo, local_directory — see docs)")
	projectResourceAddCmd.Flags().String("url", "", "Shortcut: the repo URL (only used when --type github_repo)")
	projectResourceAddCmd.Flags().String("default-branch-hint", "", "Shortcut: optional default branch hint (only used when --type github_repo)")
	projectResourceAddCmd.Flags().String("local-path", "", "Shortcut: absolute path to the working directory (only used when --type local_directory)")
	projectResourceAddCmd.Flags().String("daemon-id", "", "Shortcut: id of the daemon that owns the local path (only used when --type local_directory)")
	projectResourceAddCmd.Flags().String("ref-label", "", "Shortcut: optional label embedded in resource_ref (only used when --type local_directory)")
	projectResourceAddCmd.Flags().String("execution-mode", "", "Shortcut: how runs share the directory — in_place (default, one run at a time) or worktree (each run gets its own git worktree; requires a git repo) (only used when --type local_directory)")
	projectResourceAddCmd.Flags().String("ref", "", "Generic JSON resource_ref payload, or a github_repo checkout ref when used with --url")
	projectResourceAddCmd.Flags().String("label", "", "Optional human-readable label")
	projectResourceAddCmd.Flags().String("output", "json", "Output format: table or json")

	// project resource update — mirrors `add` flags, but every field is
	// optional so the caller can edit one thing at a time.
	projectResourceUpdateCmd.Flags().String("url", "", "Shortcut: new repo URL (github_repo)")
	projectResourceUpdateCmd.Flags().String("default-branch-hint", "", "Shortcut: new default branch hint (github_repo)")
	projectResourceUpdateCmd.Flags().String("local-path", "", "Shortcut: new absolute local path (local_directory)")
	projectResourceUpdateCmd.Flags().String("daemon-id", "", "Shortcut: new daemon id (local_directory)")
	projectResourceUpdateCmd.Flags().String("ref-label", "", "Shortcut: new label embedded in resource_ref (local_directory)")
	projectResourceUpdateCmd.Flags().String("execution-mode", "", "Shortcut: new execution mode — in_place or worktree (local_directory)")
	projectResourceUpdateCmd.Flags().String("ref", "", "Generic JSON resource_ref payload, or a github_repo checkout ref")
	projectResourceUpdateCmd.Flags().String("label", "", "New human-readable label; pass an empty string to clear")
	projectResourceUpdateCmd.Flags().Bool("clear-label", false, "Clear the human-readable label")
	projectResourceUpdateCmd.Flags().Int32("position", 0, "New display position")
	projectResourceUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// project resource remove
	projectResourceRemoveCmd.Flags().String("output", "table", "Output format: table or json")

	// project update
	projectUpdateCmd.Flags().String("title", "", "New title")
	projectUpdateCmd.Flags().String("description", "", "New description")
	projectUpdateCmd.Flags().String("status", "", "New status")
	projectUpdateCmd.Flags().String("icon", "", "New icon (emoji)")
	projectUpdateCmd.Flags().String("lead", "", "New lead name (member or agent)")
	projectUpdateCmd.Flags().String("start-date", "", "New start date (calendar day, YYYY-MM-DD; pass empty string to clear)")
	projectUpdateCmd.Flags().String("due-date", "", "New due date (calendar day, YYYY-MM-DD; pass empty string to clear)")
	projectUpdateCmd.Flags().String("visibility", "", "Switch visibility: workspace or private (its members only)")
	projectUpdateCmd.Flags().StringArray("member", nil, "Members to add with --visibility private, whether the project is switching to private or already is (name, email or user id; may be repeated)")
	projectUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// project delete
	projectDeleteCmd.Flags().String("output", "json", "Output format: table or json")

	// project status
	projectStatusCmd.Flags().String("output", "table", "Output format: table or json")

	// project archive / restore
	projectArchiveCmd.Flags().String("output", "table", "Output format: table or json")
	projectRestoreCmd.Flags().String("output", "table", "Output format: table or json")
}

// Project commands

const projectListMaxPageSize = 200

func registerProjectListFlags(cmd *cobra.Command) {
	cmd.Flags().String("output", "table", "Output format: table or json")
	cmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	cmd.Flags().StringSlice("status", nil, "Filter by status: planned, in_progress, paused, completed, cancelled (repeatable or comma-separated)")
	cmd.Flags().StringSlice("priority", nil, "Filter by priority: urgent, high, medium, low, none (repeatable or comma-separated)")
	cmd.Flags().StringSlice("lead", nil, "Filter by lead: member or agent name, me, or none (repeatable or comma-separated)")
	cmd.Flags().Bool("archived", false, "List archived projects instead of active ones")
	cmd.Flags().String("search", "", "Only projects whose title contains this text (case-insensitive)")
	cmd.Flags().String("sort", "", "Sort by: name, priority, status, progress, start_date, due_date, created_at (default) or last_activity_at")
	cmd.Flags().String("direction", "", "Sort direction, asc or desc (defaults to the natural one for --sort: newest, highest or earliest first)")
	cmd.Flags().Int("limit", 0, fmt.Sprintf("Page size, 1 to %d (default: every matching project)", projectListMaxPageSize))
	cmd.Flags().Int("offset", 0, "Number of matching projects to skip (with --limit)")
}

func runProjectList(cmd *cobra.Command, _ []string) error {
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
	for _, flag := range []string{"status", "priority"} {
		if values, _ := cmd.Flags().GetStringSlice(flag); len(values) > 0 {
			params.Set(flag, strings.Join(values, ","))
		}
	}
	if leads, _ := cmd.Flags().GetStringSlice("lead"); len(leads) > 0 {
		refs, err := resolveProjectLeadFilters(ctx, client, leads)
		if err != nil {
			return err
		}
		params.Set("lead", strings.Join(refs, ","))
	}
	if v, _ := cmd.Flags().GetString("search"); v != "" {
		params.Set("q", v)
	}
	for _, flag := range []string{"sort", "direction"} {
		if v, _ := cmd.Flags().GetString(flag); v != "" {
			params.Set(flag, v)
		}
	}
	limit, _ := cmd.Flags().GetInt("limit")
	offset, _ := cmd.Flags().GetInt("offset")
	if cmd.Flags().Changed("limit") && (limit < 1 || limit > projectListMaxPageSize) {
		return fmt.Errorf("--limit must be between 1 and %d", projectListMaxPageSize)
	}
	if offset < 0 {
		return fmt.Errorf("--offset must be 0 or more")
	}
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		params.Set("offset", strconv.Itoa(offset))
	}
	archivedOnly, _ := cmd.Flags().GetBool("archived")
	if archivedOnly {
		params.Set("include_archived", "true")
		params.Set("archived", "only")
	}

	path := "/api/projects"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	projectsRaw, _ := result["projects"].([]any)
	if archivedOnly {
		// Servers without archived=only return active projects too.
		archived := make([]any, 0, len(projectsRaw))
		for _, raw := range projectsRaw {
			if p, ok := raw.(map[string]any); ok && strVal(p, "archived_at") != "" {
				archived = append(archived, raw)
			}
		}
		projectsRaw = archived
	}
	if hasMore, _ := result["has_more"].(bool); hasMore {
		total, _ := result["total"].(float64)
		fmt.Fprintf(os.Stderr, "Showing %d–%d of %d projects; next page: --offset %d\n",
			offset+1, offset+len(projectsRaw), int(total), offset+len(projectsRaw))
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, projectsRaw)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	actors := loadActorDisplayLookup(ctx, client)
	headers := []string{"ID", "TITLE", "STATUS", "PRIORITY", "PROGRESS", "LEAD", "DUE", "LAST ACTIVITY", "CREATED"}
	rows := make([][]string, 0, len(projectsRaw))
	for _, raw := range projectsRaw {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			displayID(strVal(p, "id"), fullID),
			strVal(p, "title"),
			strVal(p, "status"),
			strVal(p, "priority"),
			formatProjectProgress(p["progress"]),
			formatLead(p, actors),
			strVal(p, "due_date"),
			dayOf(strVal(p, "last_activity_at")),
			dayOf(strVal(p, "created_at")),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// dayOf keeps the calendar day of an RFC 3339 timestamp.
func dayOf(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

// resolveProjectLeadFilters turns --lead values into the API's lead refs:
// member:<id>, agent:<id> or none.
func resolveProjectLeadFilters(ctx context.Context, client *cli.APIClient, leads []string) ([]string, error) {
	refs := make([]string, 0, len(leads))
	for _, lead := range leads {
		switch strings.ToLower(strings.TrimSpace(lead)) {
		case "none":
			refs = append(refs, "none")
		case "me":
			var me struct {
				ID string `json:"id"`
			}
			if err := client.GetJSON(ctx, "/api/me", &me); err != nil {
				return nil, fmt.Errorf("resolve lead me: %w", err)
			}
			refs = append(refs, "member:"+me.ID)
		default:
			actorType, id, err := resolveAssignee(ctx, client, lead, memberOrAgentKinds)
			if err != nil {
				return nil, fmt.Errorf("resolve lead: %w", err)
			}
			refs = append(refs, actorType+":"+id)
		}
	}
	return refs, nil
}

func runProjectGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	var project map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID, &project); err != nil {
		return fmt.Errorf("get project: %w", err)
	}

	// Breadcrumb to the resources sub-collection. Goes to stderr so JSON on
	// stdout stays parseable; the `resource_count` field on the response is
	// the programmatic equivalent. JSON numbers decode as float64.
	if n, _ := project["resource_count"].(float64); n > 0 {
		fmt.Fprintf(os.Stderr, "%d resource(s) attached — run `multica project resource list %s` to view.\n",
			int64(n), strVal(project, "id"))
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		actors := loadActorDisplayLookup(ctx, client)
		lead := formatLead(project, actors)
		headers := []string{"ID", "TITLE", "STATUS", "LEAD", "PROGRESS", "ARCHIVED", "DESCRIPTION"}
		rows := [][]string{{
			strVal(project, "id"),
			strVal(project, "title"),
			strVal(project, "status"),
			lead,
			formatProjectProgress(project["progress"]),
			formatProjectArchived(project),
			strVal(project, "description"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, project)
}

func runProjectCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"title": title}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		if err := validateProjectStatus(v); err != nil {
			return err
		}
		body["status"] = v
	}
	if v, _ := cmd.Flags().GetString("icon"); v != "" {
		body["icon"] = v
	}
	if v, _ := cmd.Flags().GetString("lead"); v != "" {
		aType, aID, resolveErr := resolveAssignee(ctx, client, v, memberOrAgentKinds)
		if resolveErr != nil {
			return fmt.Errorf("resolve lead: %w", resolveErr)
		}
		body["lead_type"] = aType
		body["lead_id"] = aID
	}
	if v, _ := cmd.Flags().GetString("start-date"); v != "" {
		body["start_date"] = v
	}
	if v, _ := cmd.Flags().GetString("due-date"); v != "" {
		body["due_date"] = v
	}
	if err := applyProjectVisibilityFlags(ctx, cmd, client, body); err != nil {
		return err
	}

	// Bundle resources into the create payload so the server attaches them in
	// the same transaction; this avoids leaving a half-attached project on
	// failure.
	repos, _ := cmd.Flags().GetStringArray("repo")
	if len(repos) > 0 {
		resources := make([]map[string]any, 0, len(repos))
		for _, repoURL := range repos {
			repoURL = strings.TrimSpace(repoURL)
			if repoURL == "" {
				continue
			}
			resources = append(resources, map[string]any{
				"resource_type": "github_repo",
				"resource_ref":  map[string]any{"url": repoURL},
			})
		}
		if len(resources) > 0 {
			body["resources"] = resources
		}
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/projects", body, &result); err != nil {
		return fmt.Errorf("create project: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "STATUS"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "title"),
			strVal(result, "status"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runProjectUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

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
		if err := validateProjectStatus(v); err != nil {
			return err
		}
		body["status"] = v
	}
	if cmd.Flags().Changed("icon") {
		v, _ := cmd.Flags().GetString("icon")
		body["icon"] = v
	}
	if cmd.Flags().Changed("lead") {
		v, _ := cmd.Flags().GetString("lead")
		aType, aID, resolveErr := resolveAssignee(ctx, client, v, memberOrAgentKinds)
		if resolveErr != nil {
			return fmt.Errorf("resolve lead: %w", resolveErr)
		}
		body["lead_type"] = aType
		body["lead_id"] = aID
	}
	// Changed() (not "") so an explicit --start-date "" reaches the server as a
	// clear, mirroring the issue update CLI.
	if cmd.Flags().Changed("start-date") {
		v, _ := cmd.Flags().GetString("start-date")
		body["start_date"] = v
	}
	if cmd.Flags().Changed("due-date") {
		v, _ := cmd.Flags().GetString("due-date")
		body["due_date"] = v
	}
	if err := applyProjectVisibilityFlags(ctx, cmd, client, body); err != nil {
		return err
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --status, --description, --icon, --lead, --start-date, --due-date, --visibility")
	}

	// Servers before MUL-9325 read member_ids only when the visibility
	// changes, so members named for a project that is already private go
	// through the member API.
	if ids, ok := body["member_ids"].([]string); ok {
		var project map[string]any
		if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID, &project); err != nil {
			return fmt.Errorf("get project: %w", err)
		}
		if strVal(project, "visibility") == "private" {
			delete(body, "member_ids")
			if len(ids) > 0 {
				if err := client.PostJSON(ctx, "/api/projects/"+projectRef.ID+"/members", map[string]any{"user_ids": ids}, nil); err != nil {
					return fmt.Errorf("add project members: %w", err)
				}
				fmt.Fprintf(os.Stderr, "Added %d member(s) to project %s.\n", len(ids), projectRef.Display)
			}
		}
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+projectRef.ID, body, &result); err != nil {
		return fmt.Errorf("update project: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "STATUS"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "title"),
			strVal(result, "status"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runProjectDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/projects/"+projectRef.ID); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Project %s deleted.\n", projectRef.Display)
	return nil
}

// formatProjectArchived renders the archive date for table output, or "-"
// for an active project.
func formatProjectArchived(project map[string]any) string {
	archivedAt := strVal(project, "archived_at")
	if archivedAt == "" {
		return "-"
	}
	if len(archivedAt) >= 10 {
		return archivedAt[:10]
	}
	return archivedAt
}

func runProjectArchive(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/projects/"+projectRef.ID+"/archive", map[string]any{}, &result); err != nil {
		return fmt.Errorf("archive project: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Project %s archived. Restore it with `multica project restore %s`.\n", strVal(result, "title"), strVal(result, "id"))
	if paused, _ := result["paused_autopilot_ids"].([]any); len(paused) > 0 {
		ids := make([]string, 0, len(paused))
		for _, id := range paused {
			if s, ok := id.(string); ok {
				ids = append(ids, s)
			}
		}
		fmt.Fprintf(os.Stderr, "Paused %d autopilot(s) that file into it: %s\n", len(ids), strings.Join(ids, ", "))
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

func runProjectRestore(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/projects/"+projectRef.ID+"/restore", map[string]any{}, &result); err != nil {
		return fmt.Errorf("restore project: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Project %s restored.\n", strVal(result, "title"))

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

func runProjectStatus(cmd *cobra.Command, args []string) error {
	id := args[0]
	status := args[1]

	if err := validateProjectStatus(status); err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, id)
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	body := map[string]any{"status": status}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+projectRef.ID, body, &result); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Project %s status changed to %s.\n", strVal(result, "title"), status)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

// Project resource commands

func runProjectResourceList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID+"/resources", &result); err != nil {
		return fmt.Errorf("list project resources: %w", err)
	}
	resourcesRaw, _ := result["resources"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resourcesRaw)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "TYPE", "REF", "LABEL"}
	rows := make([][]string, 0, len(resourcesRaw))
	for _, raw := range resourcesRaw {
		r, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			displayID(strVal(r, "id"), fullID),
			strVal(r, "resource_type"),
			summarizeResourceRef(r["resource_ref"]),
			strVal(r, "label"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runProjectResourceAdd(cmd *cobra.Command, args []string) error {
	resourceType, _ := cmd.Flags().GetString("type")
	resourceType = strings.TrimSpace(resourceType)
	if resourceType == "" {
		return fmt.Errorf("--type is required")
	}

	body := map[string]any{"resource_type": resourceType}

	// --ref takes precedence when it is JSON: any new resource type works
	// through that path without a CLI change. For github_repo only, a non-JSON
	// --ref is a checkout ref shortcut and merges with --url.
	if ref, ok, err := buildResourceRefFromRefFlag(cmd, resourceType, nil); err != nil {
		return err
	} else if ok {
		body["resource_ref"] = ref
	} else {
		switch resourceType {
		case "github_repo":
			ref, has, err := buildResourceRefFromFlags(cmd, resourceType, nil)
			if err != nil {
				return err
			}
			if !has {
				return fmt.Errorf("github_repo requires --url (or pass a JSON payload via --ref)")
			}
			body["resource_ref"] = ref
		case "local_directory":
			pathVal, _ := cmd.Flags().GetString("local-path")
			pathVal = strings.TrimSpace(pathVal)
			daemonVal, _ := cmd.Flags().GetString("daemon-id")
			daemonVal = strings.TrimSpace(daemonVal)
			if pathVal == "" || daemonVal == "" {
				return fmt.Errorf("local_directory requires --local-path and --daemon-id (or pass a JSON payload via --ref)")
			}
			ref := map[string]any{"local_path": pathVal, "daemon_id": daemonVal}
			if refLabel, _ := cmd.Flags().GetString("ref-label"); strings.TrimSpace(refLabel) != "" {
				ref["label"] = strings.TrimSpace(refLabel)
			}
			if mode, _ := cmd.Flags().GetString("execution-mode"); strings.TrimSpace(mode) != "" {
				ref["execution_mode"] = strings.TrimSpace(mode)
			}
			body["resource_ref"] = ref
		default:
			return fmt.Errorf("type %q has no built-in CLI shortcut; pass the payload via --ref '<json>'", resourceType)
		}
	}

	if label, _ := cmd.Flags().GetString("label"); label != "" {
		body["label"] = label
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/projects/"+projectRef.ID+"/resources", body, &result); err != nil {
		return fmt.Errorf("add project resource: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TYPE", "REF"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "resource_type"),
			summarizeResourceRef(result["resource_ref"]),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runProjectResourceUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	resourceRef, err := resolveProjectResourceID(ctx, client, projectRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve project resource: %w", err)
	}

	// Fetch the existing row so per-type shortcuts know which schema to
	// emit and which fields to preserve. The server treats resource_ref as
	// opaque-replace, so a partial edit like `--default-branch-hint` has to
	// rebuild the full payload here — otherwise the unmentioned `url` would
	// vanish and the server would 400.
	var existing map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID+"/resources", &existing); err != nil {
		return fmt.Errorf("list project resources: %w", err)
	}
	var resourceType string
	var existingRef map[string]any
	if list, ok := existing["resources"].([]any); ok {
		for _, raw := range list {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if strVal(row, "id") == resourceRef.ID {
				resourceType = strVal(row, "resource_type")
				if ref, ok := row["resource_ref"].(map[string]any); ok {
					existingRef = ref
				}
				break
			}
		}
	}

	body := map[string]any{}

	if ref, ok, err := buildResourceRefFromRefFlag(cmd, resourceType, existingRef); err != nil {
		return err
	} else if ok {
		body["resource_ref"] = ref
	} else {
		ref, has, err := buildResourceRefFromFlags(cmd, resourceType, existingRef)
		if err != nil {
			return err
		}
		if has {
			body["resource_ref"] = ref
		}
	}

	clearLabel, _ := cmd.Flags().GetBool("clear-label")
	if clearLabel {
		body["label"] = nil
	} else if cmd.Flags().Changed("label") {
		label, _ := cmd.Flags().GetString("label")
		body["label"] = label
	}

	if cmd.Flags().Changed("position") {
		pos, _ := cmd.Flags().GetInt32("position")
		body["position"] = pos
	}

	if len(body) == 0 {
		return fmt.Errorf("nothing to update — pass --ref / --url / --local-path / --label / --position / --clear-label")
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+projectRef.ID+"/resources/"+resourceRef.ID, body, &result); err != nil {
		return fmt.Errorf("update project resource: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TYPE", "REF", "LABEL"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "resource_type"),
			summarizeResourceRef(result["resource_ref"]),
			strVal(result, "label"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func buildResourceRefFromRefFlag(cmd *cobra.Command, resourceType string, existingRef map[string]any) (any, bool, error) {
	if !cmd.Flags().Changed("ref") {
		return nil, false, nil
	}
	rawRef, _ := cmd.Flags().GetString("ref")
	rawRef = strings.TrimSpace(rawRef)
	// --ref is the generic JSON resource_ref escape hatch. For github_repo it
	// does double duty: a JSON object/array ("{...}" / "[...]") is still the
	// escape hatch, but any other value — including bare scalars like a numeric
	// tag ("2024") or an all-digit short SHA ("1234567") — is a checkout-ref
	// shortcut that merges with --url. Only parse JSON when the value is
	// actually meant as JSON; otherwise json.Unmarshal would accept "2024" as a
	// number and silently swallow a legitimate checkout ref.
	if rawRef != "" && (resourceType != "github_repo" || looksLikeJSONPayload(rawRef)) {
		var ref any
		if err := json.Unmarshal([]byte(rawRef), &ref); err != nil {
			return nil, false, fmt.Errorf("--ref is not valid JSON: %w", err)
		}
		return ref, true, nil
	}
	if resourceType != "github_repo" {
		return nil, false, fmt.Errorf("--ref must be a JSON resource_ref payload for resource type %q", resourceType)
	}
	ref, has, err := buildResourceRefFromFlags(cmd, resourceType, existingRef)
	if err != nil {
		return nil, false, err
	}
	return ref, has, nil
}

func looksLikeJSONPayload(raw string) bool {
	raw = strings.TrimSpace(raw)
	return strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[")
}

// buildResourceRefFromFlags collects the per-type shortcut flags into a
// resource_ref payload, seeding from existingRef so partial edits (only
// --default-branch-hint, only --ref-label) preserve the unmentioned fields.
// Returns (ref, true) only when the caller actually set at least one shortcut
// flag — that lets the update command tell "no change requested" apart from
// "change ref to empty object". existingRef may be nil for the `add` path,
// where there is nothing to merge with; in that case partial inputs that miss
// required fields are still rejected.
func buildResourceRefFromFlags(cmd *cobra.Command, resourceType string, existingRef map[string]any) (map[string]any, bool, error) {
	switch resourceType {
	case "github_repo":
		urlSet := cmd.Flags().Changed("url")
		hintSet := cmd.Flags().Changed("default-branch-hint")
		refSet := cmd.Flags().Changed("ref")
		if !urlSet && !hintSet && !refSet {
			return nil, false, nil
		}
		ref := map[string]any{}
		// Seed from the existing row so a `--default-branch-hint` edit doesn't
		// clobber the `url` (server overwrites resource_ref wholesale).
		if existingRef != nil {
			if u, ok := existingRef["url"].(string); ok && strings.TrimSpace(u) != "" {
				ref["url"] = strings.TrimSpace(u)
			}
			if h, ok := existingRef["default_branch_hint"].(string); ok && strings.TrimSpace(h) != "" {
				ref["default_branch_hint"] = strings.TrimSpace(h)
			}
			if checkoutRef, ok := existingRef["ref"].(string); ok && strings.TrimSpace(checkoutRef) != "" {
				ref["ref"] = strings.TrimSpace(checkoutRef)
			}
		}
		if urlSet {
			urlVal, _ := cmd.Flags().GetString("url")
			urlVal = strings.TrimSpace(urlVal)
			if urlVal == "" {
				return nil, false, fmt.Errorf("--url cannot be empty")
			}
			ref["url"] = urlVal
		}
		if hintSet {
			hint := strings.TrimSpace(mustString(cmd, "default-branch-hint"))
			if hint == "" {
				delete(ref, "default_branch_hint")
			} else {
				ref["default_branch_hint"] = hint
			}
		}
		if refSet {
			checkoutRef := strings.TrimSpace(mustString(cmd, "ref"))
			if checkoutRef == "" {
				delete(ref, "ref")
			} else {
				ref["ref"] = checkoutRef
			}
		}
		if _, ok := ref["url"]; !ok {
			return nil, false, fmt.Errorf("github_repo: --url is required (no existing url to merge with)")
		}
		return ref, true, nil
	case "local_directory":
		pathSet := cmd.Flags().Changed("local-path")
		daemonSet := cmd.Flags().Changed("daemon-id")
		labelSet := cmd.Flags().Changed("ref-label")
		modeSet := cmd.Flags().Changed("execution-mode")
		if !pathSet && !daemonSet && !labelSet && !modeSet {
			return nil, false, nil
		}
		ref := map[string]any{}
		if existingRef != nil {
			if p, ok := existingRef["local_path"].(string); ok && strings.TrimSpace(p) != "" {
				ref["local_path"] = strings.TrimSpace(p)
			}
			if d, ok := existingRef["daemon_id"].(string); ok && strings.TrimSpace(d) != "" {
				ref["daemon_id"] = strings.TrimSpace(d)
			}
			if l, ok := existingRef["label"].(string); ok && strings.TrimSpace(l) != "" {
				ref["label"] = strings.TrimSpace(l)
			}
			if m, ok := existingRef["execution_mode"].(string); ok && strings.TrimSpace(m) != "" {
				ref["execution_mode"] = strings.TrimSpace(m)
			}
		}
		if pathSet {
			pathVal := strings.TrimSpace(mustString(cmd, "local-path"))
			if pathVal == "" {
				return nil, false, fmt.Errorf("--local-path cannot be empty")
			}
			ref["local_path"] = pathVal
		}
		if daemonSet {
			daemonVal := strings.TrimSpace(mustString(cmd, "daemon-id"))
			if daemonVal == "" {
				return nil, false, fmt.Errorf("--daemon-id cannot be empty")
			}
			ref["daemon_id"] = daemonVal
		}
		if labelSet {
			refLabel := strings.TrimSpace(mustString(cmd, "ref-label"))
			if refLabel == "" {
				delete(ref, "label")
			} else {
				ref["label"] = refLabel
			}
		}
		if modeSet {
			// An empty value clears the field back to the in_place default,
			// mirroring how --ref-label clears a label.
			mode := strings.TrimSpace(mustString(cmd, "execution-mode"))
			if mode == "" {
				delete(ref, "execution_mode")
			} else {
				ref["execution_mode"] = mode
			}
		}
		if v, ok := ref["local_path"].(string); !ok || v == "" {
			return nil, false, fmt.Errorf("local_directory: --local-path is required (no existing local_path to merge with)")
		}
		if v, ok := ref["daemon_id"].(string); !ok || v == "" {
			return nil, false, fmt.Errorf("local_directory: --daemon-id is required (no existing daemon_id to merge with)")
		}
		return ref, true, nil
	default:
		// Unknown type or empty (resource not found) — caller must use --ref.
		if cmd.Flags().Changed("url") || cmd.Flags().Changed("default-branch-hint") ||
			cmd.Flags().Changed("local-path") || cmd.Flags().Changed("daemon-id") ||
			cmd.Flags().Changed("ref-label") || cmd.Flags().Changed("execution-mode") {
			return nil, false, fmt.Errorf("no built-in shortcut for resource type %q; pass the full payload via --ref '<json>'", resourceType)
		}
		return nil, false, nil
	}
}

func mustString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}

func runProjectResourceRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	resourceRef, err := resolveProjectResourceID(ctx, client, projectRef.ID, args[1])
	if err != nil {
		return fmt.Errorf("resolve project resource: %w", err)
	}

	if err := client.DeleteJSON(ctx, "/api/projects/"+projectRef.ID+"/resources/"+resourceRef.ID); err != nil {
		return fmt.Errorf("remove project resource: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Resource %s removed from project %s.\n", resourceRef.Display, projectRef.Display)
	return nil
}

// summarizeResourceRef extracts the most useful single string from a
// resource_ref object — for github_repo this is the URL; for
// local_directory it is the local path.
func summarizeResourceRef(raw any) string {
	m, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	if u, ok := m["url"].(string); ok && u != "" {
		if ref, ok := m["ref"].(string); ok && strings.TrimSpace(ref) != "" {
			return u + " @ " + strings.TrimSpace(ref)
		}
		return u
	}
	if p, ok := m["local_path"].(string); ok && p != "" {
		return p
	}
	if data, err := json.Marshal(m); err == nil {
		return string(data)
	}
	return ""
}

// Helpers

func runProjectProgress(cmd *cobra.Command, args []string) error {
	by, _ := cmd.Flags().GetString("by")
	if by != "" && by != "assignee" && by != "label" {
		return fmt.Errorf("invalid --by %q; valid values: assignee, label", by)
	}
	history, _ := cmd.Flags().GetBool("history")
	if history && by != "" {
		return fmt.Errorf("--history and --by cannot be combined")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID+"/progress", &resp); err != nil {
		return fmt.Errorf("get project progress: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output != "table" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	switch {
	case history:
		headers, rows := projectProgressHistoryTable(resp)
		cli.PrintTable(os.Stdout, headers, rows)
	case by == "assignee":
		headers, rows := projectProgressAssigneeTable(resp, loadActorDisplayLookup(ctx, client))
		cli.PrintTable(os.Stdout, headers, rows)
	case by == "label":
		headers, rows := projectProgressLabelTable(resp, loadLabelNames(ctx, client))
		cli.PrintTable(os.Stdout, headers, rows)
	default:
		headers, rows := projectProgressSummaryTable(resp)
		cli.PrintTable(os.Stdout, headers, rows)
	}
	return nil
}

func runProjectOverview(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	base := "/api/projects/" + projectRef.ID
	var project, progress, resources, activity map[string]any
	var workingAgents []any
	if err := client.GetJSON(ctx, base, &project); err != nil {
		return fmt.Errorf("get project: %w", err)
	}
	if err := client.GetJSON(ctx, base+"/progress", &progress); err != nil {
		return fmt.Errorf("get project progress: %w", err)
	}
	if err := client.GetJSON(ctx, base+"/resources", &resources); err != nil {
		return fmt.Errorf("list project resources: %w", err)
	}
	if err := client.GetJSON(ctx, "/api/working-agents?type=issue&project="+url.QueryEscape(projectRef.ID), &workingAgents); err != nil {
		return fmt.Errorf("list working agents: %w", err)
	}
	if err := client.GetJSON(ctx, base+"/activity?limit=10", &activity); err != nil {
		return fmt.Errorf("list project activity: %w", err)
	}
	overview := map[string]any{
		"project":        project,
		"progress":       progress,
		"resources":      resources["resources"],
		"working_agents": workingAgents,
		"activity":       activity["entries"],
	}

	output, _ := cmd.Flags().GetString("output")
	if output != "table" {
		return cli.PrintJSON(os.Stdout, overview)
	}
	headers, rows := projectOverviewTable(overview, loadActorDisplayLookup(ctx, client))
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// projectOverviewTable lays the overview out as FIELD / VALUE rows.
func projectOverviewTable(overview map[string]any, actors actorDisplayLookup) ([]string, [][]string) {
	project, _ := overview["project"].(map[string]any)
	progress, _ := overview["progress"].(map[string]any)
	resources, _ := overview["resources"].([]any)
	workingAgents, _ := overview["working_agents"].([]any)
	var working []string
	for _, raw := range workingAgents {
		if a, ok := raw.(map[string]any); ok {
			working = append(working, fmt.Sprintf("%s (%d)", strVal(a, "name"), progressInt(a, "running_task_count")))
		}
	}
	_, summary := projectProgressSummaryTable(progress)
	forecast := ""
	if len(summary) == 1 {
		forecast = summary[0][len(summary[0])-1]
	}
	rows := [][]string{
		{"TITLE", strVal(project, "title")},
		{"STATUS", strVal(project, "status")},
		{"LEAD", formatLead(project, actors)},
		{"START", strVal(project, "start_date")},
		{"DUE", strVal(project, "due_date")},
		{"PROGRESS", formatProjectProgress(progress["progress"])},
		{"FORECAST", forecast},
		{"RESOURCES", fmt.Sprint(len(resources))},
		{"WORKING", strings.Join(working, ", ")},
		{"DESCRIPTION", strVal(project, "description")},
	}
	return []string{"FIELD", "VALUE"}, rows
}

func progressInt(m map[string]any, key string) int64 {
	n, _ := m[key].(float64)
	return int64(n)
}

// formatProjectProgress renders a progress object as "completed/scope (pct%)".
func formatProjectProgress(v any) string {
	p, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%d/%d (%d%%)", progressInt(p, "completed"), progressInt(p, "scope"), progressInt(p, "percent"))
}

func projectProgressSummaryTable(resp map[string]any) ([]string, [][]string) {
	p, _ := resp["progress"].(map[string]any)
	forecast := ""
	if f, ok := resp["forecast"].(map[string]any); ok {
		forecast = fmt.Sprintf("%s (%.2f/week)", strVal(f, "completion_date"), f["weekly_velocity"])
	}
	headers := []string{"PROGRESS", "SCOPE", "COMPLETED", "STARTED", "UNSTARTED", "CANCELLED", "FORECAST"}
	rows := [][]string{{
		formatProjectProgress(p),
		fmt.Sprint(progressInt(p, "scope")),
		fmt.Sprint(progressInt(p, "completed")),
		fmt.Sprint(progressInt(p, "started")),
		fmt.Sprint(progressInt(p, "unstarted")),
		fmt.Sprint(progressInt(p, "cancelled")),
		forecast,
	}}
	return headers, rows
}

func projectProgressHistoryTable(resp map[string]any) ([]string, [][]string) {
	points, _ := resp["history"].([]any)
	rows := make([][]string, 0, len(points))
	for _, raw := range points {
		p, _ := raw.(map[string]any)
		rows = append(rows, []string{
			strVal(p, "date"),
			fmt.Sprint(progressInt(p, "scope")),
			fmt.Sprint(progressInt(p, "started")),
			fmt.Sprint(progressInt(p, "completed")),
		})
	}
	return []string{"WEEK", "SCOPE", "STARTED", "COMPLETED"}, rows
}

func projectProgressAssigneeTable(resp map[string]any, actors actorDisplayLookup) ([]string, [][]string) {
	entries, _ := resp["by_assignee"].([]any)
	rows := make([][]string, 0, len(entries))
	for _, raw := range entries {
		e, _ := raw.(map[string]any)
		name := actors.actor(strVal(e, "assignee_type"), strVal(e, "assignee_id"))
		if name == "" {
			name = "unassigned"
		}
		scope, completed := progressInt(e, "scope"), progressInt(e, "completed")
		percent := int64(0)
		if scope > 0 {
			percent = completed * 100 / scope
		}
		rows = append(rows, []string{
			name,
			fmt.Sprintf("%d/%d (%d%%)", completed, scope, percent),
			fmt.Sprint(progressInt(e, "started")),
		})
	}
	return []string{"ASSIGNEE", "COMPLETED", "STARTED"}, rows
}

func projectProgressLabelTable(resp map[string]any, labels map[string]string) ([]string, [][]string) {
	entries, _ := resp["by_label"].([]any)
	rows := make([][]string, 0, len(entries))
	for _, raw := range entries {
		e, _ := raw.(map[string]any)
		name := "no label"
		if id := strVal(e, "label_id"); id != "" {
			name = labels[id]
			if name == "" {
				name = id
			}
		}
		scope, completed := progressInt(e, "scope"), progressInt(e, "completed")
		percent := int64(0)
		if scope > 0 {
			percent = completed * 100 / scope
		}
		rows = append(rows, []string{
			name,
			fmt.Sprintf("%d/%d (%d%%)", completed, scope, percent),
			fmt.Sprint(progressInt(e, "started")),
		})
	}
	return []string{"LABEL", "COMPLETED", "STARTED"}, rows
}

// loadLabelNames maps issue label ids to names. A failed read leaves the map
// empty, and rows fall back to the id.
func loadLabelNames(ctx context.Context, client *cli.APIClient) map[string]string {
	params := url.Values{"resource_type": {"issue"}}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	var result map[string]any
	names := map[string]string{}
	if err := client.GetJSON(ctx, "/api/labels?"+params.Encode(), &result); err != nil {
		return names
	}
	labels, _ := result["labels"].([]any)
	for _, raw := range labels {
		if l, ok := raw.(map[string]any); ok {
			names[strVal(l, "id")] = strVal(l, "name")
		}
	}
	return names
}

func runProjectActivity(cmd *cobra.Command, args []string) error {
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 1 {
		return fmt.Errorf("--limit must be at least 1")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	params := url.Values{"limit": {strconv.Itoa(limit)}}
	if before, _ := cmd.Flags().GetString("before"); before != "" {
		params.Set("before", before)
	}
	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID+"/activity?"+params.Encode(), &resp); err != nil {
		return fmt.Errorf("list project activity: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output != "table" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	headers, rows := projectActivityTable(resp, loadActorDisplayLookup(ctx, client))
	cli.PrintTable(os.Stdout, headers, rows)
	if next := strVal(resp, "next_cursor"); next != "" {
		fmt.Fprintf(os.Stderr, "More activity: --before %q\n", next)
	}
	return nil
}

func projectActivityTable(resp map[string]any, actors actorDisplayLookup) ([]string, [][]string) {
	entries, _ := resp["entries"].([]any)
	rows := make([][]string, 0, len(entries))
	for _, raw := range entries {
		e, _ := raw.(map[string]any)
		actor := actors.actor(strVal(e, "actor_type"), strVal(e, "actor_id"))
		if actor == "" {
			actor = strVal(e, "actor_type")
		}
		rows = append(rows, []string{
			strVal(e, "created_at"),
			strVal(e, "issue_identifier"),
			strVal(e, "issue_title"),
			actor,
			strVal(e, "action"),
			projectActivityChange(e, actors),
		})
	}
	return []string{"TIME", "ISSUE", "TITLE", "ACTOR", "ACTION", "CHANGE"}, rows
}

// projectActivityChange renders an activity's from → to, when it has one.
func projectActivityChange(entry map[string]any, actors actorDisplayLookup) string {
	details, _ := entry["details"].(map[string]any)
	from, to := strVal(details, "from"), strVal(details, "to")
	if strVal(entry, "action") == "assignee_changed" {
		from = actors.actor(strVal(details, "from_type"), strVal(details, "from_id"))
		to = actors.actor(strVal(details, "to_type"), strVal(details, "to_id"))
	}
	switch {
	case from == "" && to == "":
		return ""
	case from == "":
		return "→ " + to
	case to == "":
		return from + " →"
	default:
		return from + " → " + to
	}
}

func formatLead(project map[string]any, actors actorDisplayLookup) string {
	lType := strVal(project, "lead_type")
	lID := strVal(project, "lead_id")
	if lType == "" || lID == "" {
		return ""
	}
	return actors.actor(lType, lID)
}

// applyProjectVisibilityFlags maps --visibility / --member onto a project
// create or update body.
func applyProjectVisibilityFlags(ctx context.Context, cmd *cobra.Command, client *cli.APIClient, body map[string]any) error {
	visibility, _ := cmd.Flags().GetString("visibility")
	members, _ := cmd.Flags().GetStringArray("member")
	if visibility == "" {
		if len(members) > 0 {
			return fmt.Errorf("--member requires --visibility private")
		}
		return nil
	}
	if err := validateProjectVisibility(visibility); err != nil {
		return err
	}
	body["visibility"] = visibility
	if len(members) == 0 {
		return nil
	}
	if visibility != "private" {
		return fmt.Errorf("--member requires --visibility private")
	}
	ids, err := resolveProjectMemberUserIDs(ctx, client, members)
	if err != nil {
		return err
	}
	body["member_ids"] = ids
	return nil
}

// resolveProjectMemberUserIDs turns member names, emails or user ids into
// user ids. Only workspace members qualify.
func resolveProjectMemberUserIDs(ctx context.Context, client *cli.APIClient, refs []string) ([]string, error) {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if _, err := uuid.Parse(ref); err == nil {
			ids = append(ids, ref)
			continue
		}
		_, id, err := resolveAssignee(ctx, client, ref, memberOnlyKinds)
		if err != nil {
			return nil, fmt.Errorf("resolve member %q: %w", ref, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func printProjectMembers(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output != "table" {
		return cli.PrintJSON(os.Stdout, result)
	}
	members, _ := result["members"].([]any)
	headers := []string{"USER ID", "NAME", "EMAIL"}
	rows := make([][]string, 0, len(members))
	for _, raw := range members {
		m, _ := raw.(map[string]any)
		rows = append(rows, []string{strVal(m, "user_id"), strVal(m, "name"), strVal(m, "email")})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runProjectMemberList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectRef.ID+"/members", &result); err != nil {
		return fmt.Errorf("list project members: %w", err)
	}
	return printProjectMembers(cmd, result)
}

func runProjectMemberAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	ids, err := resolveProjectMemberUserIDs(ctx, client, args[1:])
	if err != nil {
		return err
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/projects/"+projectRef.ID+"/members", map[string]any{"user_ids": ids}, &result); err != nil {
		return fmt.Errorf("add project members: %w", err)
	}
	return printProjectMembers(cmd, result)
}

func runProjectMemberRemove(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	projectRef, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	ids, err := resolveProjectMemberUserIDs(ctx, client, args[1:2])
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("member is required")
	}
	if err := client.DeleteJSON(ctx, "/api/projects/"+projectRef.ID+"/members/"+ids[0]); err != nil {
		return fmt.Errorf("remove project member: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Removed %s from project %s.\n", args[1], projectRef.ID)
	return nil
}
