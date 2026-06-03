// CEREBRO-PATCH(cerebro-sprints-cli): FIR-2718 cerebro-only file — sprint CLI aliases project hierarchy operations.
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

var sprintCmd = &cobra.Command{
	Use:   "sprint",
	Short: "Work with sprints as sub-projects under a parent project",
	Long:  "Work with sprints as sub-projects under a parent project. Issues join a sprint by setting their Project to the sprint sub-project.",
}

var sprintListCmd = &cobra.Command{
	Use:   "list <parent-project>",
	Short: "List sprint sub-projects under a parent project",
	Args:  exactArgs(1),
	RunE:  runSprintList,
}

var sprintGetCmd = &cobra.Command{
	Use:   "get <sprint-project>",
	Short: "Get a sprint sub-project",
	Args:  exactArgs(1),
	RunE:  runSprintGet,
}

var sprintCreateCmd = &cobra.Command{
	Use:   "create <parent-project>",
	Short: "Create a sprint as a sub-project",
	Args:  exactArgs(1),
	RunE:  runSprintCreate,
}

var sprintUpdateCmd = &cobra.Command{
	Use:   "update <sprint-project>",
	Short: "Update a sprint sub-project",
	Args:  exactArgs(1),
	RunE:  runSprintUpdate,
}

var sprintDeleteCmd = &cobra.Command{
	Use:   "delete <sprint-project>",
	Short: "Delete a sprint sub-project",
	Args:  exactArgs(1),
	RunE:  runSprintDelete,
}

var sprintAssignCmd = &cobra.Command{
	Use:   "assign <issue> <sprint-project|none>",
	Short: "Move an issue into a sprint sub-project",
	Args:  exactArgs(2),
	RunE:  runSprintAssign,
}

func init() {
	sprintCmd.AddCommand(sprintListCmd)
	sprintCmd.AddCommand(sprintGetCmd)
	sprintCmd.AddCommand(sprintCreateCmd)
	sprintCmd.AddCommand(sprintUpdateCmd)
	sprintCmd.AddCommand(sprintDeleteCmd)
	sprintCmd.AddCommand(sprintAssignCmd)

	sprintListCmd.Flags().String("output", "table", "Output format: table or json")
	sprintListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	sprintGetCmd.Flags().String("output", "json", "Output format: table or json")

	sprintCreateCmd.Flags().String("name", "", "Sprint project name (required)")
	sprintCreateCmd.Flags().String("start", "", "Start date YYYY-MM-DD (stored in project description)")
	sprintCreateCmd.Flags().String("end", "", "End date YYYY-MM-DD (stored in project description)")
	sprintCreateCmd.Flags().String("goal", "", "Sprint goal (stored in project description)")
	sprintCreateCmd.Flags().String("status", "", "Status: planned, active, done, cancelled")
	sprintCreateCmd.Flags().String("output", "json", "Output format: table or json")

	sprintUpdateCmd.Flags().String("name", "", "New sprint project name")
	sprintUpdateCmd.Flags().String("start", "", "New start date YYYY-MM-DD (stored in project description)")
	sprintUpdateCmd.Flags().String("end", "", "New end date YYYY-MM-DD (stored in project description)")
	sprintUpdateCmd.Flags().String("goal", "", "New goal (stored in project description)")
	sprintUpdateCmd.Flags().String("status", "", "New status: planned, active, done, cancelled")
	sprintUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	sprintDeleteCmd.Flags().String("output", "json", "Output format: table or json")
	sprintAssignCmd.Flags().String("output", "json", "Output format: table or json")
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

func runSprintList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := newSprintContext()
	defer cancel()

	parentID, err := resolveProjectArg(ctx, client, args[0])
	if err != nil {
		return err
	}
	children, err := listSprintProjects(ctx, client, parentID)
	if err != nil {
		return err
	}

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

func runSprintGet(cmd *cobra.Command, args []string) error {
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

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+url.PathEscape(projectID), &result); err != nil {
		return fmt.Errorf("get sprint project: %w", err)
	}
	return printProjectResult(cmd, result)
}

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

	parentID, err := resolveProjectArg(ctx, client, args[0])
	if err != nil {
		return err
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

	projectID := strVal(result, "id")
	parentBody := map[string]any{"parent_project_id": parentID}
	if err := client.PutJSON(ctx, "/api/projects/"+url.PathEscape(projectID)+"/parent", parentBody, &result); err != nil {
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

	projectID, err := resolveProjectArg(ctx, client, args[0])
	if err != nil {
		return err
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
	if len(body) == 0 {
		return fmt.Errorf("nothing to update; provide --name, --status, --start, --end and/or --goal")
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

	projectID, err := resolveProjectArg(ctx, client, args[0])
	if err != nil {
		return err
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

	var projectID any
	if args[1] != "none" {
		resolved, err := resolveProjectArg(ctx, client, args[1])
		if err != nil {
			return err
		}
		projectID = resolved
	}

	body := map[string]any{"project_id": projectID}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+url.PathEscape(issueRef.ID), body, &result); err != nil {
		return fmt.Errorf("move issue to sprint project: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

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
