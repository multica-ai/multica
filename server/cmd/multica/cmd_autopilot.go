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

var autopilotCmd = &cobra.Command{
	Use:   "autopilot",
	Short: "Work with runtime autopilots",
}

var autopilotListCmd = &cobra.Command{
	Use:   "list",
	Short: "List autopilots in the workspace",
	RunE:  runAutopilotList,
}

var autopilotGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get autopilot details",
	Args:  exactArgs(1),
	RunE:  runAutopilotGet,
}

var autopilotRunsCmd = &cobra.Command{
	Use:   "runs <id>",
	Short: "List autopilot runs",
	Args:  exactArgs(1),
	RunE:  runAutopilotRuns,
}

var autopilotCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an autopilot",
	RunE:  runAutopilotCreate,
}

var autopilotUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an autopilot",
	Args:  exactArgs(1),
	RunE:  runAutopilotUpdate,
}

var autopilotTriggerCmd = &cobra.Command{
	Use:   "trigger <id>",
	Short: "Trigger an autopilot manually",
	Args:  exactArgs(1),
	RunE:  runAutopilotTrigger,
}

var autopilotDisableCmd = &cobra.Command{
	Use:   "disable <id>",
	Short: "Disable an autopilot without deleting it",
	Args:  exactArgs(1),
	RunE:  runAutopilotDisable,
}

var autopilotDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete an autopilot",
	Args:  exactArgs(1),
	RunE:  runAutopilotDelete,
}

var autopilotTriggerAddCmd = &cobra.Command{
	Use:   "trigger-add <autopilot-id>",
	Short: "Add a schedule trigger to an autopilot",
	Args:  exactArgs(1),
	RunE:  runAutopilotTriggerAdd,
}

var autopilotTriggerUpdateCmd = &cobra.Command{
	Use:   "trigger-update <autopilot-id> <trigger-id>",
	Short: "Update an autopilot schedule trigger",
	Args:  exactArgs(2),
	RunE:  runAutopilotTriggerUpdate,
}

var autopilotTriggerDeleteCmd = &cobra.Command{
	Use:   "trigger-delete <autopilot-id> <trigger-id>",
	Short: "Delete an autopilot schedule trigger",
	Args:  exactArgs(2),
	RunE:  runAutopilotTriggerDelete,
}

var validAutopilotStatuses = []string{"active", "paused"}
var validAutopilotModes = []string{"create_issue"}
var validAutopilotTriggerTypes = []string{"schedule"}

func init() {
	autopilotCmd.AddCommand(autopilotListCmd)
	autopilotCmd.AddCommand(autopilotGetCmd)
	autopilotCmd.AddCommand(autopilotRunsCmd)
	autopilotCmd.AddCommand(autopilotCreateCmd)
	autopilotCmd.AddCommand(autopilotUpdateCmd)
	autopilotCmd.AddCommand(autopilotTriggerCmd)
	autopilotCmd.AddCommand(autopilotDisableCmd)
	autopilotCmd.AddCommand(autopilotDeleteCmd)
	autopilotCmd.AddCommand(autopilotTriggerAddCmd)
	autopilotCmd.AddCommand(autopilotTriggerUpdateCmd)
	autopilotCmd.AddCommand(autopilotTriggerDeleteCmd)

	autopilotListCmd.Flags().String("output", "table", "Output format: table or json")
	autopilotListCmd.Flags().String("status", "", "Filter by status: active or paused")
	autopilotListCmd.Flags().Int("limit", 50, "Maximum number of autopilots to return")
	autopilotListCmd.Flags().Int("offset", 0, "Number of autopilots to skip")

	autopilotGetCmd.Flags().String("output", "json", "Output format: table or json")

	autopilotRunsCmd.Flags().String("output", "table", "Output format: table or json")
	autopilotRunsCmd.Flags().Int("limit", 20, "Maximum number of runs to return")
	autopilotRunsCmd.Flags().Int("offset", 0, "Number of runs to skip")

	autopilotCreateCmd.Flags().String("title", "", "Autopilot title (required)")
	autopilotCreateCmd.Flags().String("description", "", "Autopilot description")
	autopilotCreateCmd.Flags().String("agent", "", "Agent name or ID (required)")
	autopilotCreateCmd.Flags().String("mode", "create_issue", "Autopilot mode: create_issue")
	autopilotCreateCmd.Flags().String("status", "active", "Autopilot status: active or paused")
	autopilotCreateCmd.Flags().String("priority", "none", "Created issue priority: urgent, high, medium, low, or none")
	autopilotCreateCmd.Flags().String("project", "", "Project ID for created issues")
	autopilotCreateCmd.Flags().String("issue-title-template", "", "Created issue title template")
	autopilotCreateCmd.Flags().String("output", "json", "Output format: table or json")

	autopilotUpdateCmd.Flags().String("title", "", "New title")
	autopilotUpdateCmd.Flags().String("description", "", "New description")
	autopilotUpdateCmd.Flags().String("agent", "", "New agent name or ID")
	autopilotUpdateCmd.Flags().String("mode", "", "New mode: create_issue")
	autopilotUpdateCmd.Flags().String("status", "", "New status: active or paused")
	autopilotUpdateCmd.Flags().String("priority", "", "New created issue priority")
	autopilotUpdateCmd.Flags().String("project", "", "New project ID for created issues")
	autopilotUpdateCmd.Flags().String("issue-title-template", "", "New created issue title template")
	autopilotUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	autopilotTriggerCmd.Flags().String("output", "json", "Output format: table or json")

	autopilotDisableCmd.Flags().String("output", "json", "Output format: table or json")

	autopilotDeleteCmd.Flags().String("output", "table", "Output format: table or json")

	autopilotTriggerAddCmd.Flags().String("type", "schedule", "Trigger type: schedule")
	autopilotTriggerAddCmd.Flags().String("label", "", "Trigger label")
	autopilotTriggerAddCmd.Flags().String("cron", "", "Cron schedule expression (required)")
	autopilotTriggerAddCmd.Flags().String("timezone", "UTC", "IANA timezone")
	autopilotTriggerAddCmd.Flags().String("status", "active", "Trigger status: active or paused")
	autopilotTriggerAddCmd.Flags().String("output", "json", "Output format: table or json")

	autopilotTriggerUpdateCmd.Flags().String("type", "", "New trigger type: schedule")
	autopilotTriggerUpdateCmd.Flags().String("label", "", "New trigger label")
	autopilotTriggerUpdateCmd.Flags().String("cron", "", "New cron schedule expression")
	autopilotTriggerUpdateCmd.Flags().String("timezone", "", "New IANA timezone")
	autopilotTriggerUpdateCmd.Flags().String("status", "", "New trigger status: active or paused")
	autopilotTriggerUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	autopilotTriggerDeleteCmd.Flags().String("output", "table", "Output format: table or json")
}

func runAutopilotList(cmd *cobra.Command, _ []string) error {
	client, err := newAutopilotAPIClient(cmd)
	if err != nil {
		return err
	}
	status, _ := cmd.Flags().GetString("status")
	if status != "" && !isValidAutopilotChoice(status, validAutopilotStatuses) {
		return fmt.Errorf("invalid status %q; valid values: %s", status, strings.Join(validAutopilotStatuses, ", "))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("workspace_id", client.WorkspaceID)
	if status != "" {
		params.Set("status", status)
	}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetInt("offset"); v > 0 {
		params.Set("offset", fmt.Sprintf("%d", v))
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/autopilots?"+params.Encode(), &result); err != nil {
		return fmt.Errorf("list autopilots: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	autopilots, _ := result["autopilots"].([]any)
	headers := []string{"ID", "TITLE", "STATUS", "MODE", "AGENT", "PRIORITY", "UPDATED"}
	rows := make([][]string, 0, len(autopilots))
	for _, raw := range autopilots {
		a, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			truncateID(strVal(a, "id")),
			strVal(a, "title"),
			strVal(a, "status"),
			strVal(a, "mode"),
			truncateID(strVal(a, "agent_id")),
			strVal(a, "priority"),
			shortDate(strVal(a, "updated_at")),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAutopilotGet(cmd *cobra.Command, args []string) error {
	client, err := newAutopilotAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, autopilotResourcePath(args[0]), &result); err != nil {
		return fmt.Errorf("get autopilot: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "STATUS", "MODE", "AGENT", "TRIGGERS"}
		rows := [][]string{{
			truncateID(strVal(result, "id")),
			strVal(result, "title"),
			strVal(result, "status"),
			strVal(result, "mode"),
			truncateID(strVal(result, "agent_id")),
			fmt.Sprintf("%d", len(anySlice(result["triggers"]))),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runAutopilotRuns(cmd *cobra.Command, args []string) error {
	client, err := newAutopilotAPIClient(cmd)
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
	path := autopilotResourcePath(args[0]) + "/runs"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list autopilot runs: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	runs, _ := result["runs"].([]any)
	headers := []string{"ID", "SOURCE", "STATUS", "ISSUE", "TASK", "CREATED"}
	rows := make([][]string, 0, len(runs))
	for _, raw := range runs {
		run, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			truncateID(strVal(run, "id")),
			strVal(run, "source"),
			strVal(run, "status"),
			truncateID(strVal(run, "created_issue_id")),
			truncateID(strVal(run, "created_task_id")),
			shortDate(strVal(run, "created_at")),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runAutopilotCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("--title is required")
	}
	agent, _ := cmd.Flags().GetString("agent")
	if strings.TrimSpace(agent) == "" {
		return fmt.Errorf("--agent is required")
	}

	body := map[string]any{
		"title": title,
		"agent": agent,
	}
	if err := addAutopilotStringFlag(cmd, body, "description", "description", false); err != nil {
		return err
	}
	if err := addAutopilotChoiceFlag(cmd, body, "mode", "mode", validAutopilotModes, false); err != nil {
		return err
	}
	if err := addAutopilotChoiceFlag(cmd, body, "status", "status", validAutopilotStatuses, false); err != nil {
		return err
	}
	if err := addAutopilotChoiceFlag(cmd, body, "priority", "priority", validIssuePriorities(), false); err != nil {
		return err
	}
	if err := addAutopilotStringFlag(cmd, body, "project", "project", false); err != nil {
		return err
	}
	if err := addAutopilotStringFlag(cmd, body, "issue-title-template", "issue_title_template", false); err != nil {
		return err
	}

	return postAutopilotObject(cmd, "/api/autopilots", body, "create autopilot", printAutopilotObject)
}

func runAutopilotUpdate(cmd *cobra.Command, args []string) error {
	body := map[string]any{}
	if err := addAutopilotStringFlag(cmd, body, "title", "title", true); err != nil {
		return err
	}
	if err := addAutopilotStringFlag(cmd, body, "description", "description", true); err != nil {
		return err
	}
	if err := addAutopilotStringFlag(cmd, body, "agent", "agent", true); err != nil {
		return err
	}
	if err := addAutopilotChoiceFlag(cmd, body, "mode", "mode", validAutopilotModes, true); err != nil {
		return err
	}
	if err := addAutopilotChoiceFlag(cmd, body, "status", "status", validAutopilotStatuses, true); err != nil {
		return err
	}
	if err := addAutopilotChoiceFlag(cmd, body, "priority", "priority", validIssuePriorities(), true); err != nil {
		return err
	}
	if err := addAutopilotStringFlag(cmd, body, "project", "project", true); err != nil {
		return err
	}
	if err := addAutopilotStringFlag(cmd, body, "issue-title-template", "issue_title_template", true); err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --status, --description, --agent, --priority")
	}

	return putAutopilotObject(cmd, autopilotResourcePath(args[0]), body, "update autopilot", printAutopilotObject)
}

func runAutopilotTrigger(cmd *cobra.Command, args []string) error {
	return postAutopilotObject(cmd, autopilotResourcePath(args[0])+"/trigger", map[string]any{}, "trigger autopilot", printAutopilotRunObject)
}

func runAutopilotDisable(cmd *cobra.Command, args []string) error {
	return postAutopilotObject(cmd, autopilotResourcePath(args[0])+"/disable", map[string]any{}, "disable autopilot", printAutopilotObject)
}

func runAutopilotDelete(cmd *cobra.Command, args []string) error {
	client, err := newAutopilotAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, autopilotResourcePath(args[0])); err != nil {
		return fmt.Errorf("delete autopilot: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"id": args[0], "deleted": true})
	}
	fmt.Fprintf(os.Stderr, "Autopilot %s deleted.\n", truncateID(args[0]))
	return nil
}

func runAutopilotTriggerAdd(cmd *cobra.Command, args []string) error {
	cronExpr, _ := cmd.Flags().GetString("cron")
	if strings.TrimSpace(cronExpr) == "" {
		return fmt.Errorf("--cron is required")
	}
	body := map[string]any{"cron": cronExpr}
	if err := addAutopilotChoiceFlag(cmd, body, "type", "type", validAutopilotTriggerTypes, false); err != nil {
		return err
	}
	if err := addAutopilotStringFlag(cmd, body, "label", "label", false); err != nil {
		return err
	}
	if err := addAutopilotStringFlag(cmd, body, "timezone", "timezone", false); err != nil {
		return err
	}
	if err := addAutopilotChoiceFlag(cmd, body, "status", "status", validAutopilotStatuses, false); err != nil {
		return err
	}

	return postAutopilotObject(cmd, autopilotResourcePath(args[0])+"/triggers", body, "add autopilot trigger", printAutopilotTriggerObject)
}

func runAutopilotTriggerUpdate(cmd *cobra.Command, args []string) error {
	body := map[string]any{}
	if err := addAutopilotChoiceFlag(cmd, body, "type", "type", validAutopilotTriggerTypes, true); err != nil {
		return err
	}
	if err := addAutopilotStringFlag(cmd, body, "label", "label", true); err != nil {
		return err
	}
	if err := addAutopilotStringFlag(cmd, body, "cron", "cron", true); err != nil {
		return err
	}
	if err := addAutopilotStringFlag(cmd, body, "timezone", "timezone", true); err != nil {
		return err
	}
	if err := addAutopilotChoiceFlag(cmd, body, "status", "status", validAutopilotStatuses, true); err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --cron, --timezone, --status, --label")
	}

	return putAutopilotObject(cmd, autopilotResourcePath(args[0])+"/triggers/"+url.PathEscape(args[1]), body, "update autopilot trigger", printAutopilotTriggerObject)
}

func runAutopilotTriggerDelete(cmd *cobra.Command, args []string) error {
	client, err := newAutopilotAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, autopilotResourcePath(args[0])+"/triggers/"+url.PathEscape(args[1])); err != nil {
		return fmt.Errorf("delete autopilot trigger: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"id": args[1], "autopilot_id": args[0], "deleted": true})
	}
	fmt.Fprintf(os.Stderr, "Autopilot trigger %s deleted.\n", truncateID(args[1]))
	return nil
}

func newAutopilotAPIClient(cmd *cobra.Command) (*cli.APIClient, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return nil, err
		}
	}
	return client, nil
}

func postAutopilotObject(cmd *cobra.Command, path string, body map[string]any, label string, print func(*cobra.Command, map[string]any) error) error {
	client, err := newAutopilotAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return print(cmd, result)
}

func putAutopilotObject(cmd *cobra.Command, path string, body map[string]any, label string, print func(*cobra.Command, map[string]any) error) error {
	client, err := newAutopilotAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return print(cmd, result)
}

func printAutopilotObject(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "STATUS", "MODE", "AGENT"}
		rows := [][]string{{
			truncateID(strVal(result, "id")),
			strVal(result, "title"),
			strVal(result, "status"),
			strVal(result, "mode"),
			truncateID(strVal(result, "agent_id")),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func printAutopilotRunObject(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "SOURCE", "STATUS", "ISSUE", "TASK"}
		rows := [][]string{{
			truncateID(strVal(result, "id")),
			strVal(result, "source"),
			strVal(result, "status"),
			truncateID(strVal(result, "created_issue_id")),
			truncateID(strVal(result, "created_task_id")),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func printAutopilotTriggerObject(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TYPE", "STATUS", "CRON", "TIMEZONE", "NEXT RUN"}
		rows := [][]string{{
			truncateID(strVal(result, "id")),
			strVal(result, "type"),
			strVal(result, "status"),
			strVal(result, "cron"),
			strVal(result, "timezone"),
			shortDate(strVal(result, "next_run_at")),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func addAutopilotStringFlag(cmd *cobra.Command, body map[string]any, flagName, jsonName string, onlyIfChanged bool) error {
	if onlyIfChanged && !cmd.Flags().Changed(flagName) {
		return nil
	}
	value, _ := cmd.Flags().GetString(flagName)
	if onlyIfChanged || strings.TrimSpace(value) != "" {
		body[jsonName] = value
	}
	return nil
}

func addAutopilotChoiceFlag(cmd *cobra.Command, body map[string]any, flagName, jsonName string, valid []string, onlyIfChanged bool) error {
	if onlyIfChanged && !cmd.Flags().Changed(flagName) {
		return nil
	}
	value, _ := cmd.Flags().GetString(flagName)
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if !isValidAutopilotChoice(value, valid) {
		return fmt.Errorf("invalid %s %q; valid values: %s", strings.ReplaceAll(flagName, "-", "_"), value, strings.Join(valid, ", "))
	}
	body[jsonName] = value
	return nil
}

func isValidAutopilotChoice(value string, valid []string) bool {
	for _, candidate := range valid {
		if value == candidate {
			return true
		}
	}
	return false
}

func validIssuePriorities() []string {
	return []string{"urgent", "high", "medium", "low", "none"}
}

func autopilotResourcePath(id string) string {
	return "/api/autopilots/" + url.PathEscape(id)
}

func anySlice(value any) []any {
	items, _ := value.([]any)
	return items
}

func shortDate(value string) string {
	if len(value) >= 10 {
		return value[:10]
	}
	return value
}
