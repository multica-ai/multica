package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var autopilotCmd = &cobra.Command{
	Use:   "autopilot",
	Short: "Manage autopilots (scheduled/triggered agent automations)",
}

var autopilotListCmd = &cobra.Command{
	Use:   "list",
	Short: "List autopilots in the workspace",
	RunE:  runAutopilotList,
}

var autopilotGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get autopilot details (includes triggers)",
	Args:  exactArgs(1),
	RunE:  runAutopilotGet,
}

var autopilotCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new autopilot",
	RunE:  runAutopilotCreate,
}

var autopilotUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an autopilot",
	Args:  exactArgs(1),
	RunE:  runAutopilotUpdate,
}

var autopilotDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete an autopilot",
	Args:  exactArgs(1),
	RunE:  runAutopilotDelete,
}

var autopilotTriggerCmd = &cobra.Command{
	Use:   "trigger <id>",
	Short: "Manually trigger an autopilot to run once",
	Args:  exactArgs(1),
	RunE:  runAutopilotTrigger,
}

var autopilotRunsCmd = &cobra.Command{
	Use:   "runs <id>",
	Short: "List execution history for an autopilot",
	Args:  exactArgs(1),
	RunE:  runAutopilotRuns,
}

var autopilotRunMessagesCmd = &cobra.Command{
	Use:   "run-messages <run-id>",
	Short: "List messages for an autopilot execution run",
	Args:  exactArgs(1),
	RunE:  runAutopilotRunMessages,
}

var autopilotTriggerAddCmd = &cobra.Command{
	Use:   "trigger-add <autopilot-id>",
	Short: "Add a schedule trigger to an autopilot",
	Args:  exactArgs(1),
	RunE:  runAutopilotTriggerAdd,
}

var autopilotTriggerUpdateCmd = &cobra.Command{
	Use:   "trigger-update <autopilot-id> <trigger-id>",
	Short: "Update an existing trigger",
	Args:  exactArgs(2),
	RunE:  runAutopilotTriggerUpdate,
}

var autopilotTriggerDeleteCmd = &cobra.Command{
	Use:   "trigger-delete <autopilot-id> <trigger-id>",
	Short: "Delete a trigger",
	Args:  exactArgs(2),
	RunE:  runAutopilotTriggerDelete,
}

func init() {
	autopilotCmd.AddCommand(autopilotListCmd)
	autopilotCmd.AddCommand(autopilotGetCmd)
	autopilotCmd.AddCommand(autopilotCreateCmd)
	autopilotCmd.AddCommand(autopilotUpdateCmd)
	autopilotCmd.AddCommand(autopilotDeleteCmd)
	autopilotCmd.AddCommand(autopilotTriggerCmd)
	autopilotCmd.AddCommand(autopilotRunsCmd)
	autopilotCmd.AddCommand(autopilotRunMessagesCmd)
	autopilotCmd.AddCommand(autopilotTriggerAddCmd)
	autopilotCmd.AddCommand(autopilotTriggerUpdateCmd)
	autopilotCmd.AddCommand(autopilotTriggerDeleteCmd)

	// list
	autopilotListCmd.Flags().String("status", "", "Filter by status (active, paused)")
	autopilotListCmd.Flags().String("output", "table", "Output format: table or json")

	// get
	autopilotGetCmd.Flags().String("output", "json", "Output format: table or json")

	// create
	autopilotCreateCmd.Flags().String("title", "", "Autopilot title (required)")
	autopilotCreateCmd.Flags().String("description", "", "Autopilot description (used as task prompt)")
	autopilotCreateCmd.Flags().String("agent", "", "Assignee agent (name or ID) — required")
	autopilotCreateCmd.Flags().String("mode", "", "Execution mode: create_issue (required). run_only is not yet supported end-to-end.")
	autopilotCreateCmd.Flags().String("priority", "none", "Priority for created issues (none, low, medium, high, urgent)")
	autopilotCreateCmd.Flags().String("project", "", "Project ID (optional)")
	autopilotCreateCmd.Flags().String("issue-title-template", "", "Template for issue titles (create_issue mode)")
	autopilotCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// update
	autopilotUpdateCmd.Flags().String("title", "", "New title")
	autopilotUpdateCmd.Flags().String("description", "", "New description")
	autopilotUpdateCmd.Flags().String("agent", "", "New assignee agent (name or ID)")
	autopilotUpdateCmd.Flags().String("project", "", "New project ID (use empty string to clear)")
	autopilotUpdateCmd.Flags().String("priority", "", "New priority")
	autopilotUpdateCmd.Flags().String("status", "", "New status (active, paused)")
	autopilotUpdateCmd.Flags().String("mode", "", "New execution mode (create_issue)")
	autopilotUpdateCmd.Flags().String("issue-title-template", "", "New issue title template")
	autopilotUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// delete
	// (no flags)

	// trigger (manual run)
	autopilotTriggerCmd.Flags().String("output", "json", "Output format: table or json")

	// runs
	autopilotRunsCmd.Flags().Int("limit", 20, "Max number of runs to return")
	autopilotRunsCmd.Flags().Int("offset", 0, "Pagination offset")
	autopilotRunsCmd.Flags().String("output", "table", "Output format: table or json")

	// run-messages
	autopilotRunMessagesCmd.Flags().String("output", "json", "Output format: table or json")
	autopilotRunMessagesCmd.Flags().Int("since", 0, "Only return messages after this sequence number")

	// trigger-add — only schedule triggers are supported end-to-end today
	autopilotTriggerAddCmd.Flags().String("cron", "", "Cron expression (required)")
	autopilotTriggerAddCmd.Flags().String("timezone", "", "IANA timezone (default UTC)")
	autopilotTriggerAddCmd.Flags().String("label", "", "Optional human-readable label")
	autopilotTriggerAddCmd.Flags().String("output", "json", "Output format: table or json")

	// trigger-update
	autopilotTriggerUpdateCmd.Flags().Bool("enabled", true, "Enable or disable the trigger")
	autopilotTriggerUpdateCmd.Flags().String("cron", "", "New cron expression")
	autopilotTriggerUpdateCmd.Flags().String("timezone", "", "New IANA timezone")
	autopilotTriggerUpdateCmd.Flags().String("label", "", "New label")
	autopilotTriggerUpdateCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Autopilot commands
// ---------------------------------------------------------------------------

func runAutopilotList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/autopilots"
	if status, _ := cmd.Flags().GetString("status"); status != "" {
		path += "?" + url.Values{"status": {status}}.Encode()
	}

	var resp struct {
		Autopilots []map[string]any `json:"autopilots"`
		Total      int              `json:"total"`
	}
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("list autopilots: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	headers := []string{"ID", "TITLE", "STATUS", "MODE", "ASSIGNEE", "LAST_RUN"}
	rows := make([][]string, 0, len(resp.Autopilots))
	for _, a := range resp.Autopilots {
		rows = append(rows, []string{
			truncateID(strVal(a, "id")),
			strVal(a, "title"),
			strVal(a, "status"),
			strVal(a, "execution_mode"),
			truncateID(strVal(a, "assignee_id")),
			strVal(a, "last_run_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAutopilotGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/autopilots/"+args[0], &resp); err != nil {
		return fmt.Errorf("get autopilot: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	ap, _ := resp["autopilot"].(map[string]any)
	headers := []string{"ID", "TITLE", "STATUS", "MODE", "ASSIGNEE", "LAST_RUN"}
	rows := [][]string{{
		truncateID(strVal(ap, "id")),
		strVal(ap, "title"),
		strVal(ap, "status"),
		strVal(ap, "execution_mode"),
		truncateID(strVal(ap, "assignee_id")),
		strVal(ap, "last_run_at"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAutopilotCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}
	agent, _ := cmd.Flags().GetString("agent")
	if agent == "" {
		return fmt.Errorf("--agent is required (agent name or ID)")
	}
	mode, _ := cmd.Flags().GetString("mode")
	if mode == "" {
		return fmt.Errorf("--mode is required (create_issue)")
	}
	// run_only is a valid value server-side but the dispatch path is not wired
	// end-to-end (daemon /start resolves workspace only via issue/chat, and the
	// agent prompt expects an issue ID). Keep the CLI to create_issue until the
	// server path is fixed to avoid shipping a mode that returns 404 on start.
	if mode != "create_issue" {
		return fmt.Errorf("--mode must be create_issue (run_only is not yet supported end-to-end)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	agentID, err := resolveAgent(ctx, client, agent)
	if err != nil {
		return fmt.Errorf("resolve agent: %w", err)
	}

	body := map[string]any{
		"title":          title,
		"assignee_id":    agentID,
		"execution_mode": mode,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if cmd.Flags().Changed("priority") {
		v, _ := cmd.Flags().GetString("priority")
		body["priority"] = v
	}
	if v, _ := cmd.Flags().GetString("project"); v != "" {
		body["project_id"] = v
	}
	if v, _ := cmd.Flags().GetString("issue-title-template"); v != "" {
		body["issue_title_template"] = v
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/autopilots", body, &result); err != nil {
		return fmt.Errorf("create autopilot: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Autopilot created: %s (%s)\n", strVal(result, "title"), strVal(result, "id"))
	return nil
}

func runAutopilotUpdate(cmd *cobra.Command, args []string) error {
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
	if cmd.Flags().Changed("agent") {
		v, _ := cmd.Flags().GetString("agent")
		agentID, resolveErr := resolveAgent(ctx, client, v)
		if resolveErr != nil {
			return fmt.Errorf("resolve agent: %w", resolveErr)
		}
		body["assignee_id"] = agentID
	}
	if cmd.Flags().Changed("project") {
		v, _ := cmd.Flags().GetString("project")
		if v == "" {
			body["project_id"] = nil
		} else {
			body["project_id"] = v
		}
	}
	if cmd.Flags().Changed("priority") {
		v, _ := cmd.Flags().GetString("priority")
		body["priority"] = v
	}
	if cmd.Flags().Changed("status") {
		v, _ := cmd.Flags().GetString("status")
		body["status"] = v
	}
	if cmd.Flags().Changed("mode") {
		v, _ := cmd.Flags().GetString("mode")
		if v != "create_issue" {
			return fmt.Errorf("--mode must be create_issue (run_only is not yet supported end-to-end)")
		}
		body["execution_mode"] = v
	}
	if cmd.Flags().Changed("issue-title-template") {
		v, _ := cmd.Flags().GetString("issue-title-template")
		body["issue_title_template"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --description, --agent, --status, --mode, etc.")
	}

	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/autopilots/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update autopilot: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Autopilot updated: %s (%s)\n", strVal(result, "title"), strVal(result, "id"))
	return nil
}

func runAutopilotDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/autopilots/"+args[0]); err != nil {
		return fmt.Errorf("delete autopilot: %w", err)
	}
	fmt.Printf("Autopilot %s deleted.\n", args[0])
	return nil
}

func runAutopilotTrigger(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var run map[string]any
	if err := client.PostJSON(ctx, "/api/autopilots/"+args[0]+"/trigger", nil, &run); err != nil {
		return fmt.Errorf("trigger autopilot: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, run)
	}
	fmt.Printf("Autopilot triggered: run %s (status: %s)\n", strVal(run, "id"), strVal(run, "status"))
	return nil
}

func runAutopilotRuns(cmd *cobra.Command, args []string) error {
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
	path := "/api/autopilots/" + args[0] + "/runs"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var resp struct {
		Runs  []map[string]any `json:"runs"`
		Total int              `json:"total"`
	}
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("list runs: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	headers := []string{"ID", "SOURCE", "STATUS", "ISSUE", "TRIGGERED_AT", "COMPLETED_AT"}
	rows := make([][]string, 0, len(resp.Runs))
	for _, r := range resp.Runs {
		rows = append(rows, []string{
			truncateID(strVal(r, "id")),
			strVal(r, "source"),
			strVal(r, "status"),
			truncateID(strVal(r, "issue_id")),
			strVal(r, "triggered_at"),
			strVal(r, "completed_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAutopilotTriggerAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	// Only schedule triggers are dispatched end-to-end today. The server
	// schema also defines "webhook" and "api" kinds, but no inbound endpoint
	// fires them — they'd sit in the DB forever. Re-add kind selection here
	// when those paths are implemented.
	cron, _ := cmd.Flags().GetString("cron")
	if cron == "" {
		return fmt.Errorf("--cron is required")
	}

	body := map[string]any{
		"kind":            "schedule",
		"cron_expression": cron,
	}
	if v, _ := cmd.Flags().GetString("timezone"); v != "" {
		body["timezone"] = v
	}
	if v, _ := cmd.Flags().GetString("label"); v != "" {
		body["label"] = v
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/autopilots/"+args[0]+"/triggers", body, &result); err != nil {
		return fmt.Errorf("create trigger: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Trigger created: %s (kind=%s)\n", strVal(result, "id"), strVal(result, "kind"))
	return nil
}

func runAutopilotTriggerUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{}
	if cmd.Flags().Changed("enabled") {
		v, _ := cmd.Flags().GetBool("enabled")
		body["enabled"] = v
	}
	if cmd.Flags().Changed("cron") {
		v, _ := cmd.Flags().GetString("cron")
		body["cron_expression"] = v
	}
	if cmd.Flags().Changed("timezone") {
		v, _ := cmd.Flags().GetString("timezone")
		body["timezone"] = v
	}
	if cmd.Flags().Changed("label") {
		v, _ := cmd.Flags().GetString("label")
		body["label"] = v
	}
	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --enabled, --cron, --timezone, or --label")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	path := "/api/autopilots/" + args[0] + "/triggers/" + args[1]
	if err := client.PatchJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("update trigger: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Trigger updated: %s\n", strVal(result, "id"))
	return nil
}

func runAutopilotTriggerDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/autopilots/" + args[0] + "/triggers/" + args[1]
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("delete trigger: %w", err)
	}
	fmt.Printf("Trigger %s deleted.\n", args[1])
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// uuidRegexp matches a canonical UUID (8-4-4-4-12 hex).
var uuidRegexp = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// resolveAgent accepts either a UUID or an agent name (case-insensitive substring)
// and returns the agent's UUID. Errors on no match or ambiguous match.
func resolveAgent(ctx context.Context, client *cli.APIClient, nameOrID string) (string, error) {
	if uuidRegexp.MatchString(nameOrID) {
		return nameOrID, nil
	}
	if client.WorkspaceID == "" {
		return "", fmt.Errorf("workspace ID is required to resolve agents; use --workspace-id or set MULTICA_WORKSPACE_ID")
	}

	var agents []map[string]any
	agentPath := "/api/agents?" + url.Values{"workspace_id": {client.WorkspaceID}}.Encode()
	if err := client.GetJSON(ctx, agentPath, &agents); err != nil {
		return "", fmt.Errorf("fetch agents: %w", err)
	}

	nameLower := strings.ToLower(nameOrID)
	type match struct{ ID, Name string }
	var matches []match
	for _, a := range agents {
		aName := strVal(a, "name")
		if strings.Contains(strings.ToLower(aName), nameLower) {
			matches = append(matches, match{ID: strVal(a, "id"), Name: aName})
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no agent found matching %q", nameOrID)
	case 1:
		return matches[0].ID, nil
	default:
		var parts []string
		for _, m := range matches {
			parts = append(parts, fmt.Sprintf("  %q (%s)", m.Name, truncateID(m.ID)))
		}
		return "", fmt.Errorf("ambiguous agent %q; matches:\n%s", nameOrID, strings.Join(parts, "\n"))
	}
}

func runAutopilotRunMessages(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Fetch the run to get its task_id.
	var run map[string]any
	if err := client.GetJSON(ctx, "/api/autopilots/runs/"+args[0], &run); err != nil {
		return fmt.Errorf("get run: %w", err)
	}

	taskID := strVal(run, "task_id")
	if taskID == "" {
		return fmt.Errorf("run %s has no associated task (may still be pending)", args[0])
	}

	// Fetch messages via the task messages endpoint.
	path := "/api/daemon/tasks/" + taskID + "/messages"
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
