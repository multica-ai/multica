// CEREBRO-PATCH(cerebro-sprints-cli): FIR-2718 cerebro-only file — sprint CLI.
//
// FIR-2500: rewired to the real sprint feature (cerebro_sprint rows — the
// sprints users see in the app, served under /api/cerebro/sprints). The
// pre-FIR-2500 behavior treated a sprint as a sub-project; arguments that
// resolve to a project instead of a sprint still fall back to that legacy
// path so existing automation does not break.
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var sprintCmd = &cobra.Command{
	Use:   "sprint",
	Short: "Work with sprints (the sprints shown in the app)",
	Long: "Work with sprints — the same sprints users see in the app.\n" +
		"Sprints belong to a project; `sprint list` without arguments shows every sprint in the workspace,\n" +
		"so you can find the active sprint without knowing the parent project.\n" +
		"Arguments that resolve to a project instead of a sprint use the legacy sub-project sprint model.",
}

var sprintListCmd = &cobra.Command{
	Use:   "list [project]",
	Short: "List sprints — all workspace sprints, or one project's sprints",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSprintList,
}

var sprintGetCmd = &cobra.Command{
	Use:   "get <sprint>",
	Short: "Get a sprint by ID, ID prefix, or name",
	Args:  exactArgs(1),
	RunE:  runSprintGet,
}

var sprintIssuesCmd = &cobra.Command{
	Use:   "issues <sprint>",
	Short: "List the issues in a sprint with their status",
	Args:  exactArgs(1),
	RunE:  runSprintIssues,
}

var sprintCreateCmd = &cobra.Command{
	Use:   "create <project>",
	Short: "Create a sprint in a project",
	Args:  exactArgs(1),
	RunE:  runSprintCreate,
}

var sprintUpdateCmd = &cobra.Command{
	Use:   "update <sprint>",
	Short: "Update a sprint",
	Args:  exactArgs(1),
	RunE:  runSprintUpdate,
}

var sprintDeleteCmd = &cobra.Command{
	Use:   "delete <sprint>",
	Short: "Delete a sprint",
	Args:  exactArgs(1),
	RunE:  runSprintDelete,
}

var sprintAssignCmd = &cobra.Command{
	Use:   "assign <issue> <sprint|none>",
	Short: "Put an issue in a sprint (or remove it with `none`)",
	Args:  exactArgs(2),
	RunE:  runSprintAssign,
}

func init() {
	sprintCmd.AddCommand(sprintListCmd)
	sprintCmd.AddCommand(sprintGetCmd)
	sprintCmd.AddCommand(sprintIssuesCmd)
	sprintCmd.AddCommand(sprintCreateCmd)
	sprintCmd.AddCommand(sprintUpdateCmd)
	sprintCmd.AddCommand(sprintDeleteCmd)
	sprintCmd.AddCommand(sprintAssignCmd)

	sprintListCmd.Flags().String("status", "", "Filter by status: planned, active, done, cancelled")
	sprintListCmd.Flags().String("output", "table", "Output format: table or json")
	sprintListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	sprintGetCmd.Flags().String("output", "json", "Output format: table or json")

	sprintIssuesCmd.Flags().String("output", "table", "Output format: table or json")
	sprintIssuesCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	sprintCreateCmd.Flags().String("name", "", "Sprint name (required)")
	sprintCreateCmd.Flags().String("start", "", "Start date YYYY-MM-DD")
	sprintCreateCmd.Flags().String("end", "", "End date YYYY-MM-DD")
	sprintCreateCmd.Flags().String("goal", "", "Sprint goal")
	sprintCreateCmd.Flags().String("status", "", "Status: planned, active, done, cancelled")
	sprintCreateCmd.Flags().String("output", "json", "Output format: table or json")

	sprintUpdateCmd.Flags().String("name", "", "New sprint name")
	sprintUpdateCmd.Flags().String("start", "", "New start date YYYY-MM-DD")
	sprintUpdateCmd.Flags().String("end", "", "New end date YYYY-MM-DD")
	sprintUpdateCmd.Flags().String("goal", "", "New goal")
	sprintUpdateCmd.Flags().String("status", "", "New status: planned, active, done, cancelled")
	sprintUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	sprintDeleteCmd.Flags().String("output", "json", "Output format: table or json")
	sprintAssignCmd.Flags().String("output", "json", "Output format: table or json")

	// FIR-2500: create an issue directly in a sprint. The flag lives on the
	// upstream `issue create` command; the assignment itself runs from a
	// small marked patch in cmd_issue.go after the issue is created.
	issueCreateCmd.Flags().String("sprint", "", "Sprint to place the new issue in (ID, ID prefix, or name)")
}

func newSprintContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
}

func resolveProjectArg(ctx context.Context, client *cli.APIClient, arg string) (string, error) {
	ref, err := resolveProjectID(ctx, client, arg)
	if err != nil {
		return "", fmt.Errorf("resolve project: %w", err)
	}
	return ref.ID, nil
}

// ---------------------------------------------------------------------------
// Sprint resolution
// ---------------------------------------------------------------------------

func isHTTPStatus(err error, status int) bool {
	var httpErr *cli.HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == status
}

func fetchWorkspaceSprints(ctx context.Context, client *cli.APIClient, status string) ([]map[string]any, error) {
	path := "/api/cerebro/sprints"
	if status != "" {
		path += "?status=" + url.QueryEscape(status)
	}
	var result struct {
		Sprints []map[string]any `json:"sprints"`
	}
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return nil, err
	}
	return result.Sprints, nil
}

// resolveSprint resolves a user-supplied sprint reference to a sprint object.
// Accepted forms: full UUID, unique UUID prefix, or unique (case-insensitive)
// sprint name. Returns found=false when nothing in the workspace matches —
// callers decide whether that is an error or a signal to try the legacy
// sub-project path.
func resolveSprint(ctx context.Context, client *cli.APIClient, arg string) (map[string]any, bool, error) {
	trimmed := strings.TrimSpace(arg)
	if trimmed == "" {
		return nil, false, fmt.Errorf("sprint is required")
	}
	if uuidRegexp.MatchString(trimmed) {
		var sprint map[string]any
		err := client.GetJSON(ctx, "/api/cerebro/sprints/"+url.PathEscape(trimmed), &sprint)
		if err == nil {
			return sprint, true, nil
		}
		if !isHTTPStatus(err, http.StatusNotFound) {
			return nil, false, fmt.Errorf("get sprint: %w", err)
		}
		return nil, false, nil
	}

	sprints, err := fetchWorkspaceSprints(ctx, client, "")
	if err != nil {
		return nil, false, fmt.Errorf("list sprints: %w", err)
	}
	lowered := strings.ToLower(trimmed)
	normalized := strings.ToLower(strings.ReplaceAll(trimmed, "-", ""))
	var matches []map[string]any
	for _, sprint := range sprints {
		id := strings.ToLower(strings.ReplaceAll(strVal(sprint, "id"), "-", ""))
		name := strings.ToLower(strVal(sprint, "name"))
		if name == lowered || (len(normalized) >= 6 && strings.HasPrefix(id, normalized)) {
			matches = append(matches, sprint)
		}
	}
	switch len(matches) {
	case 0:
		return nil, false, nil
	case 1:
		return matches[0], true, nil
	default:
		names := make([]string, 0, len(matches))
		for _, m := range matches {
			names = append(names, fmt.Sprintf("%s (%s, project %s)", strVal(m, "name"), strVal(m, "id"), strVal(m, "project_title")))
		}
		return nil, false, fmt.Errorf("sprint %q is ambiguous, matches: %s", trimmed, strings.Join(names, "; "))
	}
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func runSprintList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := newSprintContext()
	defer cancel()

	status, _ := cmd.Flags().GetString("status")
	status = strings.TrimSpace(status)

	if len(args) == 0 {
		sprints, err := fetchWorkspaceSprints(ctx, client, status)
		if err != nil {
			return fmt.Errorf("list sprints: %w", err)
		}
		return printSprintList(cmd, sprints, true)
	}

	projectID, err := resolveProjectArg(ctx, client, args[0])
	if err != nil {
		return err
	}
	var result struct {
		Sprints []map[string]any `json:"sprints"`
	}
	if err := client.GetJSON(ctx, "/api/cerebro/projects/"+url.PathEscape(projectID)+"/sprints", &result); err != nil {
		return fmt.Errorf("list project sprints: %w", err)
	}
	if len(result.Sprints) > 0 {
		if status != "" {
			filtered := result.Sprints[:0]
			for _, sprint := range result.Sprints {
				if strVal(sprint, "status") == status {
					filtered = append(filtered, sprint)
				}
			}
			result.Sprints = filtered
		}
		return printSprintList(cmd, result.Sprints, false)
	}

	// Legacy fallback: the project has no real sprints — list sub-projects
	// the way the pre-FIR-2500 sprint CLI did.
	children, err := listSprintProjects(ctx, client, projectID)
	if err != nil {
		return err
	}
	if len(children) > 0 {
		fmt.Fprintln(os.Stderr, "Note: this project has no sprints; showing legacy sub-projects. The app's sprint feature is not enabled here.")
		return printLegacySprintProjectList(cmd, children)
	}
	return printSprintList(cmd, nil, false)
}

func printSprintList(cmd *cobra.Command, sprints []map[string]any, withProject bool) error {
	if sprints == nil {
		sprints = []map[string]any{}
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"sprints": sprints})
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "NAME", "STATUS", "START", "END"}
	if withProject {
		headers = []string{"ID", "NAME", "PROJECT", "STATUS", "START", "END"}
	}
	rows := make([][]string, 0, len(sprints))
	for _, sprint := range sprints {
		row := []string{
			displayID(strVal(sprint, "id"), fullID),
			strVal(sprint, "name"),
		}
		if withProject {
			row = append(row, strVal(sprint, "project_title"))
		}
		row = append(row, strVal(sprint, "status"), strVal(sprint, "start_date"), strVal(sprint, "end_date"))
		rows = append(rows, row)
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func printLegacySprintProjectList(cmd *cobra.Command, children []map[string]any) error {
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, children)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "TITLE", "STATUS", "ISSUES", "DONE"}
	rows := make([][]string, 0, len(children))
	for _, sprint := range children {
		rows = append(rows, []string{
			displayID(strVal(sprint, "id"), fullID),
			strVal(sprint, "title"),
			projectStatusToSprintStatus(strVal(sprint, "status")),
			numStr(sprint, "issue_count"),
			numStr(sprint, "done_count"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// get
// ---------------------------------------------------------------------------

func runSprintGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := newSprintContext()
	defer cancel()

	sprint, found, err := resolveSprint(ctx, client, args[0])
	if err != nil {
		return err
	}
	if found {
		return printSprintResult(cmd, sprint)
	}

	// Legacy fallback: the argument may be a sub-project acting as a sprint.
	projectID, projErr := resolveProjectArg(ctx, client, args[0])
	if projErr != nil {
		return fmt.Errorf("no sprint matches %q (also tried as a legacy sprint project: %v)", args[0], projErr)
	}
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+url.PathEscape(projectID), &result); err != nil {
		return fmt.Errorf("get sprint project: %w", err)
	}
	return printProjectResult(cmd, result)
}

func printSprintResult(cmd *cobra.Command, sprint map[string]any) error {
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		headers := []string{"ID", "NAME", "STATUS", "START", "END", "GOAL"}
		rows := [][]string{{
			strVal(sprint, "id"),
			strVal(sprint, "name"),
			strVal(sprint, "status"),
			strVal(sprint, "start_date"),
			strVal(sprint, "end_date"),
			strVal(sprint, "goal"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, sprint)
}

// ---------------------------------------------------------------------------
// issues
// ---------------------------------------------------------------------------

func runSprintIssues(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := newSprintContext()
	defer cancel()

	sprint, found, err := resolveSprint(ctx, client, args[0])
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no sprint matches %q; run `multica sprint list` to see the workspace's sprints", args[0])
	}

	sprintID := strVal(sprint, "id")
	var result struct {
		Issues []map[string]any `json:"issues"`
	}
	if err := client.GetJSON(ctx, "/api/cerebro/sprints/"+url.PathEscape(sprintID)+"/issues", &result); err != nil {
		return fmt.Errorf("list sprint issues: %w", err)
	}
	if result.Issues == nil {
		result.Issues = []map[string]any{}
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"sprint": sprint, "issues": result.Issues})
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"KEY", "TITLE", "STATUS", "PRIORITY"}
	rows := make([][]string, 0, len(result.Issues))
	for _, issue := range result.Issues {
		key := strVal(issue, "identifier")
		if key == "" {
			key = displayID(strVal(issue, "issue_id"), fullID)
		}
		rows = append(rows, []string{
			key,
			strVal(issue, "title"),
			strVal(issue, "status"),
			strVal(issue, "priority"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// create / update / delete
// ---------------------------------------------------------------------------

func runSprintCreate(cmd *cobra.Command, args []string) error {
	name, _ := cmd.Flags().GetString("name")
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("--name is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := newSprintContext()
	defer cancel()

	projectID, err := resolveProjectArg(ctx, client, args[0])
	if err != nil {
		return err
	}

	// A project with sprint settings uses the real sprint feature; without
	// them we keep the legacy sub-project behavior.
	var settings map[string]any
	settingsErr := client.GetJSON(ctx, "/api/cerebro/projects/"+url.PathEscape(projectID)+"/sprint-settings", &settings)
	if settingsErr == nil {
		start := sprintFlagString(cmd, "start")
		end := sprintFlagString(cmd, "end")
		if start == "" || end == "" {
			return fmt.Errorf("--start and --end (YYYY-MM-DD) are required to create a sprint in this project")
		}
		body := map[string]any{
			"name":       name,
			"start_date": start,
			"end_date":   end,
		}
		if goal := sprintFlagString(cmd, "goal"); goal != "" {
			body["goal"] = goal
		}
		if status := sprintFlagString(cmd, "status"); status != "" {
			body["status"] = status
		}
		var sprint map[string]any
		if err := client.PostJSON(ctx, "/api/cerebro/projects/"+url.PathEscape(projectID)+"/sprints", body, &sprint); err != nil {
			return fmt.Errorf("create sprint: %w", err)
		}
		return printSprintResult(cmd, sprint)
	}
	if !isHTTPStatus(settingsErr, http.StatusNotFound) {
		return fmt.Errorf("load sprint settings: %w", settingsErr)
	}

	body := map[string]any{
		"title":       name,
		"status":      sprintStatusToProjectStatus(sprintFlagString(cmd, "status")),
		"description": sprintProjectDescription(cmd),
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/projects", body, &result); err != nil {
		return fmt.Errorf("create sprint project: %w", err)
	}
	legacyProjectID := strVal(result, "id")
	parentBody := map[string]any{"parent_project_id": projectID}
	if err := client.PutJSON(ctx, "/api/projects/"+url.PathEscape(legacyProjectID)+"/parent", parentBody, &result); err != nil {
		return fmt.Errorf("set sprint project parent: %w", err)
	}
	return printProjectResult(cmd, result)
}

func runSprintUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := newSprintContext()
	defer cancel()

	if !cmd.Flags().Changed("name") && !cmd.Flags().Changed("status") &&
		!cmd.Flags().Changed("start") && !cmd.Flags().Changed("end") && !cmd.Flags().Changed("goal") {
		return fmt.Errorf("nothing to update; provide --name, --status, --start, --end and/or --goal")
	}

	sprint, found, err := resolveSprint(ctx, client, args[0])
	if err != nil {
		return err
	}
	if found {
		body := map[string]any{}
		if cmd.Flags().Changed("name") {
			body["name"] = sprintFlagString(cmd, "name")
		}
		if cmd.Flags().Changed("status") {
			body["status"] = sprintFlagString(cmd, "status")
		}
		if cmd.Flags().Changed("start") {
			body["start_date"] = sprintFlagString(cmd, "start")
		}
		if cmd.Flags().Changed("end") {
			body["end_date"] = sprintFlagString(cmd, "end")
		}
		if cmd.Flags().Changed("goal") {
			body["goal"] = sprintFlagString(cmd, "goal")
		}
		var updated map[string]any
		if err := client.PutJSON(ctx, "/api/cerebro/sprints/"+url.PathEscape(strVal(sprint, "id")), body, &updated); err != nil {
			return fmt.Errorf("update sprint: %w", err)
		}
		return printSprintResult(cmd, updated)
	}

	projectID, projErr := resolveProjectArg(ctx, client, args[0])
	if projErr != nil {
		return fmt.Errorf("no sprint matches %q (also tried as a legacy sprint project: %v)", args[0], projErr)
	}
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		body["title"] = sprintFlagString(cmd, "name")
	}
	if cmd.Flags().Changed("status") {
		body["status"] = sprintStatusToProjectStatus(sprintFlagString(cmd, "status"))
	}
	if cmd.Flags().Changed("start") || cmd.Flags().Changed("end") || cmd.Flags().Changed("goal") {
		body["description"] = sprintProjectDescription(cmd)
	}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+url.PathEscape(projectID), body, &result); err != nil {
		return fmt.Errorf("update sprint project: %w", err)
	}
	return printProjectResult(cmd, result)
}

func runSprintDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := newSprintContext()
	defer cancel()

	sprint, found, err := resolveSprint(ctx, client, args[0])
	if err != nil {
		return err
	}
	if found {
		sprintID := strVal(sprint, "id")
		if err := client.DeleteJSON(ctx, "/api/cerebro/sprints/"+url.PathEscape(sprintID)); err != nil {
			return fmt.Errorf("delete sprint: %w", err)
		}
		if output, _ := cmd.Flags().GetString("output"); output == "json" {
			return cli.PrintJSON(os.Stdout, map[string]any{"id": sprintID, "deleted": true})
		}
		fmt.Fprintf(os.Stdout, "Sprint %s deleted.\n", strVal(sprint, "name"))
		return nil
	}

	projectID, projErr := resolveProjectArg(ctx, client, args[0])
	if projErr != nil {
		return fmt.Errorf("no sprint matches %q (also tried as a legacy sprint project: %v)", args[0], projErr)
	}
	if err := client.DeleteJSON(ctx, "/api/projects/"+url.PathEscape(projectID)); err != nil {
		return fmt.Errorf("delete sprint project: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"id": projectID, "deleted": true})
	}
	fmt.Fprintf(os.Stdout, "Sprint project %s deleted.\n", projectID)
	return nil
}

// ---------------------------------------------------------------------------
// assign
// ---------------------------------------------------------------------------

func runSprintAssign(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := newSprintContext()
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	if args[1] == "none" {
		if err := putIssueSprint(ctx, client, issueRef.ID, ""); err != nil {
			return fmt.Errorf("remove issue from sprint: %w", err)
		}
		return cli.PrintJSON(os.Stdout, map[string]any{"issue_id": issueRef.ID, "sprint_id": nil, "removed": true})
	}

	sprint, found, err := resolveSprint(ctx, client, args[1])
	if err != nil {
		return err
	}
	if found {
		sprintID := strVal(sprint, "id")
		if err := putIssueSprint(ctx, client, issueRef.ID, sprintID); err != nil {
			return fmt.Errorf("assign issue to sprint: %w", err)
		}
		return cli.PrintJSON(os.Stdout, map[string]any{
			"issue_id":    issueRef.ID,
			"sprint_id":   sprintID,
			"sprint_name": strVal(sprint, "name"),
			"assigned":    true,
		})
	}

	// Legacy fallback: the target may be a sub-project acting as a sprint —
	// the pre-FIR-2500 behavior moved the issue into that project.
	projectID, projErr := resolveProjectArg(ctx, client, args[1])
	if projErr != nil {
		return fmt.Errorf("no sprint matches %q (also tried as a legacy sprint project: %v)", args[1], projErr)
	}
	body := map[string]any{"project_id": projectID}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+url.PathEscape(issueRef.ID), body, &result); err != nil {
		return fmt.Errorf("move issue to sprint project: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func putIssueSprint(ctx context.Context, client *cli.APIClient, issueID, sprintID string) error {
	body := map[string]any{"sprint_id": sprintID}
	return client.PutJSON(ctx, "/api/cerebro/issues/"+url.PathEscape(issueID)+"/sprint", body, nil)
}

// assignCreatedIssueToUISprint backs the `issue create --sprint` flag
// (FIR-2500). Called from a CEREBRO-PATCH in cmd_issue.go after the issue is
// created; a failure must not fail the command (the issue already exists, and
// a non-zero exit would invite a duplicate-creating retry), so the caller
// prints the returned error as a warning.
func assignCreatedIssueToUISprint(ctx context.Context, cmd *cobra.Command, client *cli.APIClient, issueID string) error {
	arg, _ := cmd.Flags().GetString("sprint")
	arg = strings.TrimSpace(arg)
	if arg == "" || issueID == "" {
		return nil
	}
	sprint, found, err := resolveSprint(ctx, client, arg)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no sprint matches %q; assign manually with `multica sprint assign`", arg)
	}
	sprintID := strVal(sprint, "id")
	if err := putIssueSprint(ctx, client, issueID, sprintID); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Issue placed in sprint %s (%s)\n", strVal(sprint, "name"), sprintID)
	return nil
}

// ---------------------------------------------------------------------------
// Shared helpers (legacy sub-project sprint model)
// ---------------------------------------------------------------------------

func listSprintProjects(ctx context.Context, client *cli.APIClient, parentID string) ([]map[string]any, error) {
	var tree map[string]any
	if err := client.GetJSON(ctx, "/api/projects/tree", &tree); err != nil {
		return nil, fmt.Errorf("list project tree: %w", err)
	}
	rawProjects, _ := tree["projects"].([]any)
	children := make([]map[string]any, 0)
	for _, raw := range rawProjects {
		project, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strVal(project, "parent_project_id") == parentID {
			children = append(children, project)
		}
	}
	return children, nil
}

func printProjectResult(cmd *cobra.Command, result map[string]any) error {
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		headers := []string{"ID", "TITLE", "STATUS"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "title"),
			projectStatusToSprintStatus(strVal(result, "status")),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func sprintProjectDescription(cmd *cobra.Command) string {
	parts := []string{}
	if v := sprintFlagString(cmd, "start"); v != "" {
		parts = append(parts, "Start: "+v)
	}
	if v := sprintFlagString(cmd, "end"); v != "" {
		parts = append(parts, "End: "+v)
	}
	if v := sprintFlagString(cmd, "goal"); v != "" {
		parts = append(parts, "Goal: "+v)
	}
	return strings.Join(parts, "\n")
}

func sprintFlagString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return strings.TrimSpace(v)
}

func sprintStatusToProjectStatus(status string) string {
	switch status {
	case "active":
		return "in_progress"
	case "done":
		return "completed"
	case "cancelled":
		return "cancelled"
	default:
		return "planned"
	}
}

func projectStatusToSprintStatus(status string) string {
	switch status {
	case "in_progress":
		return "active"
	case "completed":
		return "done"
	case "cancelled":
		return "cancelled"
	default:
		return "planned"
	}
}

func numStr(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case float64:
		return fmt.Sprintf("%d", int64(v))
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}
