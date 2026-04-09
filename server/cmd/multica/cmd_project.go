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

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Work with projects",
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List projects in the workspace",
	RunE:  runProjectList,
}

var projectGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get project details",
	Args:  exactArgs(1),
	RunE:  runProjectGet,
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

var projectStatusCmd = &cobra.Command{
	Use:   "status <id> <status>",
	Short: "Change project status",
	Args:  exactArgs(2),
	RunE:  runProjectStatus,
}

var validProjectStatuses = []string{
	"planned", "in_progress", "paused", "completed", "cancelled",
}

func init() {
	projectCmd.AddCommand(projectListCmd)
	projectCmd.AddCommand(projectGetCmd)
	projectCmd.AddCommand(projectCreateCmd)
	projectCmd.AddCommand(projectUpdateCmd)
	projectCmd.AddCommand(projectDeleteCmd)
	projectCmd.AddCommand(projectStatusCmd)

	// project list
	projectListCmd.Flags().String("output", "table", "Output format: table or json")
	projectListCmd.Flags().String("status", "", "Filter by status")

	// project get
	projectGetCmd.Flags().String("output", "json", "Output format: table or json")

	// project create
	projectCreateCmd.Flags().String("title", "", "Project title (required)")
	projectCreateCmd.Flags().String("description", "", "Project description")
	projectCreateCmd.Flags().String("status", "", "Project status")
	projectCreateCmd.Flags().String("icon", "", "Project icon (emoji)")
	projectCreateCmd.Flags().String("lead", "", "Lead name (member or agent)")
	projectCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// project update
	projectUpdateCmd.Flags().String("title", "", "New title")
	projectUpdateCmd.Flags().String("description", "", "New description")
	projectUpdateCmd.Flags().String("status", "", "New status")
	projectUpdateCmd.Flags().String("icon", "", "New icon (emoji)")
	projectUpdateCmd.Flags().String("lead", "", "New lead name (member or agent)")
	projectUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// project delete
	projectDeleteCmd.Flags().String("output", "json", "Output format: table or json")

	// project status
	projectStatusCmd.Flags().String("output", "table", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Project commands
// ---------------------------------------------------------------------------

func runProjectList(cmd *cobra.Command, _ []string) error {
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

	path := "/api/projects"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	projectsRaw, _ := result["projects"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, projectsRaw)
	}

	headers := []string{"ID", "TITLE", "STATUS", "LEAD", "CREATED"}
	rows := make([][]string, 0, len(projectsRaw))
	for _, raw := range projectsRaw {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		lead := formatLead(p)
		created := strVal(p, "created_at")
		if len(created) >= 10 {
			created = created[:10]
		}
		rows = append(rows, []string{
			truncateID(strVal(p, "id")),
			strVal(p, "title"),
			strVal(p, "status"),
			lead,
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runProjectGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var project map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+args[0], &project); err != nil {
		return fmt.Errorf("get project: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		lead := formatLead(project)
		headers := []string{"ID", "TITLE", "STATUS", "LEAD", "DESCRIPTION"}
		rows := [][]string{{
			truncateID(strVal(project, "id")),
			strVal(project, "title"),
			strVal(project, "status"),
			lead,
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"title": title}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("status"); v != "" {
		body["status"] = v
	}
	if v, _ := cmd.Flags().GetString("icon"); v != "" {
		body["icon"] = v
	}
	if v, _ := cmd.Flags().GetString("lead"); v != "" {
		aType, aID, resolveErr := resolveAssignee(ctx, client, v)
		if resolveErr != nil {
			return fmt.Errorf("resolve lead: %w", resolveErr)
		}
		body["lead_type"] = aType
		body["lead_id"] = aID
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/projects", body, &result); err != nil {
		return fmt.Errorf("create project: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "STATUS"}
		rows := [][]string{{
			truncateID(strVal(result, "id")),
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
	if cmd.Flags().Changed("icon") {
		v, _ := cmd.Flags().GetString("icon")
		body["icon"] = v
	}
	if cmd.Flags().Changed("lead") {
		v, _ := cmd.Flags().GetString("lead")
		aType, aID, resolveErr := resolveAssignee(ctx, client, v)
		if resolveErr != nil {
			return fmt.Errorf("resolve lead: %w", resolveErr)
		}
		body["lead_type"] = aType
		body["lead_id"] = aID
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --status, --description, --icon, --lead")
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update project: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "STATUS"}
		rows := [][]string{{
			truncateID(strVal(result, "id")),
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/projects/"+args[0]); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Project %s deleted.\n", truncateID(args[0]))
	return nil
}

func runProjectStatus(cmd *cobra.Command, args []string) error {
	id := args[0]
	status := args[1]

	valid := false
	for _, s := range validProjectStatuses {
		if s == status {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("invalid status %q; valid values: %s", status, strings.Join(validProjectStatuses, ", "))
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"status": status}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+id, body, &result); err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Project %s status changed to %s.\n", truncateID(id), status)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func formatLead(project map[string]any) string {
	lType := strVal(project, "lead_type")
	lID := strVal(project, "lead_id")
	if lType == "" || lID == "" {
		return ""
	}
	return lType + ":" + truncateID(lID)
}
