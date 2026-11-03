package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Workflow commands wrap /api/issue-workflows and the project workflow
// switch. Permission checks stay on the server: edits and deletes are limited
// to the workflow owner and workspace admins, switching a project's workflow
// to admins and the project lead. The CLI reports the server's error as is.

var workflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Work with issue workflows (statuses and their handoffs)",
}

var workflowListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workflows in the workspace",
	RunE:  runWorkflowList,
}

var workflowGetCmd = &cobra.Command{
	Use:   "get <workflow>",
	Short: "Get a workflow and its steps (accepts an ID, ID prefix, exact name, or \"default\")",
	Args:  exactArgs(1),
	RunE:  runWorkflowGet,
}

var workflowCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a workflow",
	Long: `Create a workflow.

--steps-file takes a JSON array of steps in board order, the same shape the
API accepts, e.g.:

  [
    {"status_key": "todo"},
    {"status_key": "in_review", "handler": {"type": "agent", "id": "<agent-id>"},
     "instructions": "Review the PR", "next_status_key": "done"},
    {"new_status": {"name": "QA", "category": "started"}},
    {"status_key": "done"}
  ]

Todo, done and cancelled are added when missing.`,
	RunE: runWorkflowCreate,
}

var workflowUpdateCmd = &cobra.Command{
	Use:   "update <workflow>",
	Short: "Update a workflow's name, description, or steps",
	Long: `Update a workflow's name, description, or steps.

--steps-file replaces all steps (same JSON shape as "workflow create"). When
the new steps drop a status that issues still use, map each dropped status
with --map old=new. Use --dry-run to print the migration plan without writing.`,
	Args: exactArgs(1),
	RunE: runWorkflowUpdate,
}

var workflowDeleteCmd = &cobra.Command{
	Use:   "delete <workflow>",
	Short: "Delete a workflow (rejected while a project still uses it)",
	Args:  exactArgs(1),
	RunE:  runWorkflowDelete,
}

var workflowHandoffCmd = &cobra.Command{
	Use:   "handoff",
	Short: "Manage what happens when an issue enters a workflow status",
}

var workflowHandoffSetCmd = &cobra.Command{
	Use:   "set <workflow> <status-key>",
	Short: "Set the handler, instructions, and next/back status for one step",
	Long: `Set the handoff of one step. Only the flags you pass change; the rest of
the step is kept.

--handler accepts none, project_lead, creator, or a member, agent, or squad
name or UUID.`,
	Args: exactArgs(2),
	RunE: runWorkflowHandoffSet,
}

var projectWorkflowCmd = &cobra.Command{
	Use:   "workflow",
	Short: "Show or switch the workflow a project uses",
}

var projectWorkflowGetCmd = &cobra.Command{
	Use:   "get <project-id>",
	Short: "Show the project's effective workflow (Default when none is set)",
	Args:  exactArgs(1),
	RunE:  runProjectWorkflowGet,
}

var projectWorkflowSetCmd = &cobra.Command{
	Use:   "set <project-id> <workflow|default>",
	Short: "Switch the project to another workflow",
	Long: `Switch the project to another workflow, or back to the workspace Default.

Issues on statuses the new workflow does not list must be mapped with
--map old=new. Use --dry-run to print the migration plan first. Moving issues
this way neither reassigns them nor starts runs.`,
	Args: exactArgs(2),
	RunE: runProjectWorkflowSet,
}

func init() {
	workflowCmd.AddCommand(workflowListCmd)
	workflowCmd.AddCommand(workflowGetCmd)
	workflowCmd.AddCommand(workflowCreateCmd)
	workflowCmd.AddCommand(workflowUpdateCmd)
	workflowCmd.AddCommand(workflowDeleteCmd)
	workflowCmd.AddCommand(workflowHandoffCmd)
	workflowHandoffCmd.AddCommand(workflowHandoffSetCmd)

	projectCmd.AddCommand(projectWorkflowCmd)
	projectWorkflowCmd.AddCommand(projectWorkflowGetCmd)
	projectWorkflowCmd.AddCommand(projectWorkflowSetCmd)

	workflowListCmd.Flags().String("output", "table", "Output format: table or json")
	workflowListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	workflowGetCmd.Flags().String("output", "json", "Output format: table or json")

	workflowCreateCmd.Flags().String("name", "", "Workflow name (required)")
	workflowCreateCmd.Flags().String("description", "", "Workflow description")
	workflowCreateCmd.Flags().String("steps-file", "", "Path to a JSON array of steps")
	workflowCreateCmd.Flags().String("output", "json", "Output format: table or json")

	workflowUpdateCmd.Flags().String("name", "", "New name")
	workflowUpdateCmd.Flags().String("description", "", "New description")
	workflowUpdateCmd.Flags().String("steps-file", "", "Path to a JSON array of steps; replaces all steps")
	workflowUpdateCmd.Flags().StringArray("map", nil, "Move issues off a dropped status: old=new (repeatable)")
	workflowUpdateCmd.Flags().Bool("dry-run", false, "Print the migration plan without writing")
	workflowUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	workflowDeleteCmd.Flags().String("output", "table", "Output format: table or json")

	workflowHandoffSetCmd.Flags().String("handler", "", "none, project_lead, creator, or a member/agent/squad name or UUID")
	workflowHandoffSetCmd.Flags().String("instructions", "", "Instructions the handler's run gets")
	workflowHandoffSetCmd.Flags().String("instructions-file", "", "Read instructions from a file")
	workflowHandoffSetCmd.Flags().String("next", "", "Status the handler moves the issue to when done (empty to clear)")
	workflowHandoffSetCmd.Flags().String("back", "", "Status the handler sends the issue back to (empty to clear)")
	workflowHandoffSetCmd.Flags().String("copy-for-project", "", "Save on a copy of the workflow used only by this project")
	workflowHandoffSetCmd.Flags().String("output", "json", "Output format: table or json")

	projectWorkflowGetCmd.Flags().String("output", "json", "Output format: table or json")

	projectWorkflowSetCmd.Flags().StringArray("map", nil, "Move issues off a status the new workflow lacks: old=new (repeatable)")
	projectWorkflowSetCmd.Flags().Bool("dry-run", false, "Print the migration plan without writing")
	projectWorkflowSetCmd.Flags().String("output", "json", "Output format: table or json")
}

// --- helpers ---

func listWorkflows(ctx context.Context, client *cli.APIClient) ([]map[string]any, error) {
	path := "/api/issue-workflows"
	if client.WorkspaceID != "" {
		path += "?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
	}
	var result struct {
		Workflows []map[string]any `json:"workflows"`
	}
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return nil, err
	}
	return result.Workflows, nil
}

// resolveWorkflow accepts "default", a full UUID, an exact name
// (case-insensitive), or a unique ID prefix.
func resolveWorkflow(ctx context.Context, client *cli.APIClient, input string) (map[string]any, error) {
	ref := strings.TrimSpace(input)
	if ref == "" {
		return nil, fmt.Errorf("workflow is required")
	}
	if uuidRegexp.MatchString(ref) {
		var wf map[string]any
		if err := client.GetJSON(ctx, "/api/issue-workflows/"+ref, &wf); err != nil {
			return nil, err
		}
		return wf, nil
	}
	workflows, err := listWorkflows(ctx, client)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(ref, "default") {
		for _, wf := range workflows {
			if b, _ := wf["is_default"].(bool); b {
				return wf, nil
			}
		}
	}
	var byName []map[string]any
	for _, wf := range workflows {
		if strings.EqualFold(strVal(wf, "name"), ref) {
			byName = append(byName, wf)
		}
	}
	if len(byName) == 1 {
		return byName[0], nil
	}
	if len(byName) > 1 {
		return nil, fmt.Errorf("more than one workflow is named %q; pass its ID", ref)
	}
	prefix, err := normalizeUUIDPrefix(ref)
	if err != nil {
		return nil, fmt.Errorf("no workflow named %q", ref)
	}
	var matches []idCandidate
	var found map[string]any
	for _, wf := range workflows {
		if strings.HasPrefix(compactUUID(strVal(wf, "id")), prefix) {
			matches = append(matches, idCandidate{ID: strVal(wf, "id")})
			found = wf
		}
	}
	switch len(matches) {
	case 0:
		return nil, fmt.Errorf("no workflow found matching %q; run `multica workflow list --full-id`", ref)
	case 1:
		return found, nil
	default:
		return nil, ambiguousIDPrefixError("workflow", ref, matches)
	}
}

// parseStatusMapping turns repeated old=new flags into a mapping.
func parseStatusMapping(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		from, to, ok := strings.Cut(p, "=")
		from, to = strings.TrimSpace(from), strings.TrimSpace(to)
		if !ok || from == "" || to == "" {
			return nil, fmt.Errorf("invalid --map %q; expected old=new", p)
		}
		if prev, dup := out[from]; dup && prev != to {
			return nil, fmt.Errorf("--map maps %q twice (%q and %q)", from, prev, to)
		}
		out[from] = to
	}
	return out, nil
}

func readStepsFile(path string) ([]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read --steps-file: %w", err)
	}
	var steps []any
	if err := json.Unmarshal(data, &steps); err != nil {
		return nil, fmt.Errorf("--steps-file must be a JSON array of steps: %w", err)
	}
	return steps, nil
}

func workflowSteps(wf map[string]any) []map[string]any {
	raw, _ := wf["steps"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, s := range raw {
		if m, ok := s.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func formatStepHandler(step map[string]any) string {
	h, _ := step["handler"].(map[string]any)
	typ := strVal(h, "type")
	if typ == "" {
		typ = "none"
	}
	if id := strVal(h, "id"); id != "" {
		return typ + ":" + id
	}
	return typ
}

func printWorkflowTable(wf map[string]any) {
	fmt.Fprintf(os.Stdout, "%s (%s)\n", strVal(wf, "name"), strVal(wf, "id"))
	headers := []string{"STATUS", "HANDLER", "NEXT", "BACK"}
	steps := workflowSteps(wf)
	rows := make([][]string, 0, len(steps))
	for _, s := range steps {
		rows = append(rows, []string{
			strVal(s, "status_key"),
			formatStepHandler(s),
			strVal(s, "next_status_key"),
			strVal(s, "back_status_key"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func printMappingPlan(plan map[string]any) {
	fmt.Fprintf(os.Stdout, "Dry run: %v of %v issue(s) would move.\n", plan["affected_issues"], plan["total_issues"])
	if required, _ := plan["required"].([]any); len(required) > 0 {
		printMappingRequired(plan)
	}
}

// reportMappingRequired handles the server's 409
// workflow_status_mapping_required. The default error formatter keeps only
// the first message line, which drops the plan a script needs to supply every
// mapping at once. This prints the full body (JSON) or the plan (table) to
// stdout, the server message to stderr, and returns errSilent so the command
// still exits non-zero (1, as for any other conflict) without printing twice. Any other error returns nil and is left to the
// caller.
func reportMappingRequired(cmd *cobra.Command, err error) error {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusConflict {
		return nil
	}
	var body map[string]any
	if json.Unmarshal([]byte(httpErr.Body), &body) != nil || strVal(body, "code") != "workflow_status_mapping_required" {
		return nil
	}
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		if plan, ok := body["plan"].(map[string]any); ok {
			printMappingRequired(plan)
		}
	} else if perr := cli.PrintJSON(os.Stdout, body); perr != nil {
		return perr
	}
	fmt.Fprintf(os.Stderr, "Error: %s; pass --map old=new for each status listed\n", strVal(body, "error"))
	return errSilent
}

func printMappingRequired(plan map[string]any) {
	required, _ := plan["required"].([]any)
	headers := []string{"FROM STATUS", "ISSUES", "SUGGESTED"}
	rows := make([][]string, 0, len(required))
	for _, raw := range required {
		r, _ := raw.(map[string]any)
		rows = append(rows, []string{strVal(r, "status_key"), fmt.Sprint(r["issue_count"]), strVal(r, "suggested_status_key")})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

// resolveHandoffHandler maps the --handler value to a handler payload.
func resolveHandoffHandler(ctx context.Context, client *cli.APIClient, value string) (map[string]any, error) {
	v := strings.TrimSpace(value)
	switch strings.ToLower(v) {
	case "", "none":
		return map[string]any{"type": "none"}, nil
	case "project_lead", "creator":
		return map[string]any{"type": strings.ToLower(v)}, nil
	}
	var typ, id string
	var err error
	if uuidRegexp.MatchString(v) {
		typ, id, err = resolveAssigneeByID(ctx, client, v, issueAssigneeKinds)
	} else {
		typ, id, err = resolveAssignee(ctx, client, v, issueAssigneeKinds)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve handler: %w", err)
	}
	return map[string]any{"type": typ, "id": id}, nil
}

// --- workflow commands ---

func runWorkflowList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	workflows, err := listWorkflows(ctx, client)
	if err != nil {
		return fmt.Errorf("list workflows: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, workflows)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "NAME", "DEFAULT", "STEPS", "PROJECTS"}
	rows := make([][]string, 0, len(workflows))
	for _, wf := range workflows {
		isDefault := ""
		if b, _ := wf["is_default"].(bool); b {
			isDefault = "yes"
		}
		projects, _ := wf["project_ids"].([]any)
		rows = append(rows, []string{
			displayID(strVal(wf, "id"), fullID),
			strVal(wf, "name"),
			isDefault,
			strconv.Itoa(len(workflowSteps(wf))),
			strconv.Itoa(len(projects)),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runWorkflowGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	wf, err := resolveWorkflow(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("get workflow: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		printWorkflowTable(wf)
		return nil
	}
	return cli.PrintJSON(os.Stdout, wf)
}

func runWorkflowCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("--name is required")
	}
	body := map[string]any{"name": name}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if path, _ := cmd.Flags().GetString("steps-file"); path != "" {
		steps, err := readStepsFile(path)
		if err != nil {
			return err
		}
		body["steps"] = steps
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issue-workflows", body, &result); err != nil {
		return fmt.Errorf("create workflow: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		printWorkflowTable(result)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runWorkflowUpdate(cmd *cobra.Command, args []string) error {
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		body["name"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if path, _ := cmd.Flags().GetString("steps-file"); path != "" {
		steps, err := readStepsFile(path)
		if err != nil {
			return err
		}
		body["steps"] = steps
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to update; provide --name, --description, and/or --steps-file")
	}
	pairs, _ := cmd.Flags().GetStringArray("map")
	mapping, err := parseStatusMapping(pairs)
	if err != nil {
		return err
	}
	if mapping != nil {
		body["status_mapping"] = mapping
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if dryRun {
		body["dry_run"] = true
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	wf, err := resolveWorkflow(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve workflow: %w", err)
	}
	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/issue-workflows/"+strVal(wf, "id"), body, &result); err != nil {
		if reported := reportMappingRequired(cmd, err); reported != nil {
			return reported
		}
		return fmt.Errorf("update workflow: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		if plan, ok := result["plan"].(map[string]any); ok && dryRun {
			printMappingPlan(plan)
			return nil
		}
		printWorkflowTable(result)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runWorkflowDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	wf, err := resolveWorkflow(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve workflow: %w", err)
	}
	id := strVal(wf, "id")
	if err := client.DeleteJSON(ctx, "/api/issue-workflows/"+id); err != nil {
		return fmt.Errorf("delete workflow: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"id": id, "deleted": true})
	}
	fmt.Fprintf(os.Stdout, "Workflow %s deleted.\n", strVal(wf, "name"))
	return nil
}

func runWorkflowHandoffSet(cmd *cobra.Command, args []string) error {
	statusKey := strings.TrimSpace(args[1])
	if cmd.Flags().Changed("instructions") && cmd.Flags().Changed("instructions-file") {
		return fmt.Errorf("use either --instructions or --instructions-file, not both")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	wf, err := resolveWorkflow(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve workflow: %w", err)
	}
	var current map[string]any
	for _, s := range workflowSteps(wf) {
		if strVal(s, "status_key") == statusKey {
			current = s
			break
		}
	}
	if current == nil {
		return fmt.Errorf("workflow %s has no step %q", strVal(wf, "name"), statusKey)
	}

	// The API replaces the whole handoff; start from the current step so
	// only the flags passed change.
	body := map[string]any{
		"handler":         current["handler"],
		"instructions":    strVal(current, "instructions"),
		"next_status_key": strVal(current, "next_status_key"),
		"back_status_key": strVal(current, "back_status_key"),
	}
	if cmd.Flags().Changed("handler") {
		v, _ := cmd.Flags().GetString("handler")
		handler, err := resolveHandoffHandler(ctx, client, v)
		if err != nil {
			return err
		}
		body["handler"] = handler
	}
	if cmd.Flags().Changed("instructions") {
		v, _ := cmd.Flags().GetString("instructions")
		body["instructions"] = v
	}
	if path, _ := cmd.Flags().GetString("instructions-file"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read --instructions-file: %w", err)
		}
		body["instructions"] = string(data)
	}
	if cmd.Flags().Changed("next") {
		v, _ := cmd.Flags().GetString("next")
		body["next_status_key"] = v
	}
	if cmd.Flags().Changed("back") {
		v, _ := cmd.Flags().GetString("back")
		body["back_status_key"] = v
	}
	if v, _ := cmd.Flags().GetString("copy-for-project"); v != "" {
		projectRef, err := resolveProjectID(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve project: %w", err)
		}
		body["copy_for_project_id"] = projectRef.ID
	}

	var result map[string]any
	path := "/api/issue-workflows/" + strVal(wf, "id") + "/handoffs/" + url.PathEscape(statusKey)
	if err := client.PutJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("set handoff: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		printWorkflowTable(result)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

// --- project workflow commands ---

func runProjectWorkflowGet(cmd *cobra.Command, args []string) error {
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
	ref := strVal(project, "workflow_id")
	usesDefault := ref == ""
	if usesDefault {
		ref = "default"
	}
	wf, err := resolveWorkflow(ctx, client, ref)
	if err != nil {
		return fmt.Errorf("get workflow: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		if usesDefault {
			fmt.Fprintln(os.Stdout, "Project uses the workspace Default workflow.")
		}
		printWorkflowTable(wf)
		return nil
	}
	return cli.PrintJSON(os.Stdout, map[string]any{
		"project_id":   projectRef.ID,
		"uses_default": usesDefault,
		"workflow":     wf,
	})
}

func runProjectWorkflowSet(cmd *cobra.Command, args []string) error {
	pairs, _ := cmd.Flags().GetStringArray("map")
	mapping, err := parseStatusMapping(pairs)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

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
	// workflow_id null switches back to Default.
	body := map[string]any{"workflow_id": nil}
	if !strings.EqualFold(strings.TrimSpace(args[1]), "default") {
		wf, err := resolveWorkflow(ctx, client, args[1])
		if err != nil {
			return fmt.Errorf("resolve workflow: %w", err)
		}
		body["workflow_id"] = strVal(wf, "id")
	}
	if mapping != nil {
		body["status_mapping"] = mapping
	}
	if dryRun {
		body["dry_run"] = true
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/projects/"+projectRef.ID+"/workflow", body, &result); err != nil {
		if reported := reportMappingRequired(cmd, err); reported != nil {
			return reported
		}
		return fmt.Errorf("set project workflow: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		plan, _ := result["plan"].(map[string]any)
		if dryRun {
			printMappingPlan(plan)
			return nil
		}
		fmt.Fprintf(os.Stdout, "Project workflow changed; %v issue(s) moved.\n", result["updated_issues"])
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}
