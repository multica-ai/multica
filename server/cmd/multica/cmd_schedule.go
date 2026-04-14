package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var scheduleCmd = &cobra.Command{
	Use:   "schedule",
	Short: "Manage scheduled tasks (cron-driven issue creation)",
}

var scheduleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List scheduled tasks in the workspace",
	RunE:  runScheduleList,
}

var scheduleGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get schedule details",
	Args:  exactArgs(1),
	RunE:  runScheduleGet,
}

var scheduleCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new scheduled task",
	RunE:  runScheduleCreate,
}

var scheduleUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a scheduled task",
	Args:  exactArgs(1),
	RunE:  runScheduleUpdate,
}

var scheduleDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a scheduled task",
	Args:  exactArgs(1),
	RunE:  runScheduleDelete,
}

var scheduleRunCmd = &cobra.Command{
	Use:   "run <id>",
	Short: "Fire a scheduled task immediately (creates an issue without waiting for cron)",
	Args:  exactArgs(1),
	RunE:  runScheduleRun,
}

func init() {
	scheduleCmd.AddCommand(scheduleListCmd)
	scheduleCmd.AddCommand(scheduleGetCmd)
	scheduleCmd.AddCommand(scheduleCreateCmd)
	scheduleCmd.AddCommand(scheduleUpdateCmd)
	scheduleCmd.AddCommand(scheduleDeleteCmd)
	scheduleCmd.AddCommand(scheduleRunCmd)

	// schedule list
	scheduleListCmd.Flags().String("output", "table", "Output format: table or json")

	// schedule get
	scheduleGetCmd.Flags().String("output", "json", "Output format: table or json")

	// schedule create
	scheduleCreateCmd.Flags().String("name", "", "Schedule name (required)")
	scheduleCreateCmd.Flags().String("cron", "", "Cron expression, 5-field POSIX (required). E.g. '0 9 * * *' or '@daily'")
	scheduleCreateCmd.Flags().String("timezone", "UTC", "IANA timezone for the cron expression")
	scheduleCreateCmd.Flags().String("title", "", "Issue title template (required). Supports {{date}}, {{datetime}}, {{schedule_name}}")
	scheduleCreateCmd.Flags().String("description", "", "Issue description")
	scheduleCreateCmd.Flags().String("assignee", "", "Agent or member name to assign issues to (required)")
	scheduleCreateCmd.Flags().String("priority", "none", "Issue priority (none, urgent, high, medium, low)")
	scheduleCreateCmd.Flags().Bool("enabled", true, "Enable the schedule immediately")
	scheduleCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// schedule update
	scheduleUpdateCmd.Flags().String("name", "", "New name")
	scheduleUpdateCmd.Flags().String("cron", "", "New cron expression")
	scheduleUpdateCmd.Flags().String("timezone", "", "New timezone")
	scheduleUpdateCmd.Flags().String("title", "", "New title template")
	scheduleUpdateCmd.Flags().String("description", "", "New description")
	scheduleUpdateCmd.Flags().String("assignee", "", "New assignee (agent or member name)")
	scheduleUpdateCmd.Flags().String("priority", "", "New priority")
	scheduleUpdateCmd.Flags().String("enabled", "", "Enable/disable (true/false)")
	scheduleUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// schedule run
	scheduleRunCmd.Flags().String("output", "json", "Output format: table or json")
}

type scheduleRow struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	CronExpression string  `json:"cron_expression"`
	Timezone       string  `json:"timezone"`
	Enabled        bool    `json:"enabled"`
	NextRunAt      string  `json:"next_run_at"`
	LastRunAt      *string `json:"last_run_at"`
	LastRunIssueID *string `json:"last_run_issue_id"`
	LastRunError   *string `json:"last_run_error"`
	AssigneeType   string  `json:"assignee_type"`
	AssigneeID     string  `json:"assignee_id"`
	TitleTemplate  string  `json:"title_template"`
	Description    string  `json:"description"`
	Priority       string  `json:"priority"`
	CreatedAt      string  `json:"created_at"`
}

func runScheduleList(cmd *cobra.Command, args []string) error {
	c, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	wsID := resolveWorkspaceID(cmd)
	var rows []scheduleRow
	if err := c.GetJSON(context.Background(), fmt.Sprintf("/api/schedules?workspace_id=%s", wsID), &rows); err != nil {
		return err
	}
	format, _ := cmd.Flags().GetString("output")
	if format == "json" {
		return cli.PrintJSON(os.Stdout, rows)
	}
	headers := []string{"ID", "NAME", "CRON", "TZ", "ENABLED", "NEXT_RUN", "LAST_RUN"}
	tableRows := make([][]string, len(rows))
	for i, s := range rows {
		enabled := "yes"
		if !s.Enabled {
			enabled = "no"
		}
		lastRun := "—"
		if s.LastRunAt != nil {
			lastRun = formatTime(*s.LastRunAt)
		}
		tableRows[i] = []string{shortID(s.ID), s.Name, s.CronExpression, s.Timezone, enabled, formatTime(s.NextRunAt), lastRun}
	}
	cli.PrintTable(os.Stdout, headers, tableRows)
	return nil
}

func runScheduleGet(cmd *cobra.Command, args []string) error {
	c, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var row scheduleRow
	if err := c.GetJSON(context.Background(), "/api/schedules/"+args[0], &row); err != nil {
		return err
	}
	format, _ := cmd.Flags().GetString("output")
	if format == "json" {
		return cli.PrintJSON(os.Stdout, row)
	}
	fmt.Printf("ID:    %s\n", row.ID)
	fmt.Printf("Name:  %s\n", row.Name)
	fmt.Printf("Cron:  %s (%s)\n", row.CronExpression, row.Timezone)
	fmt.Printf("Next:  %s\n", formatTime(row.NextRunAt))
	enabled := "yes"
	if !row.Enabled {
		enabled = "no"
	}
	fmt.Printf("Enabled: %s\n", enabled)
	return nil
}

func runScheduleCreate(cmd *cobra.Command, args []string) error {
	c, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	wsID := resolveWorkspaceID(cmd)

	name, _ := cmd.Flags().GetString("name")
	cron, _ := cmd.Flags().GetString("cron")
	tz, _ := cmd.Flags().GetString("timezone")
	title, _ := cmd.Flags().GetString("title")
	desc, _ := cmd.Flags().GetString("description")
	assignee, _ := cmd.Flags().GetString("assignee")
	priority, _ := cmd.Flags().GetString("priority")
	enabled, _ := cmd.Flags().GetBool("enabled")

	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if cron == "" {
		return fmt.Errorf("--cron is required")
	}
	if title == "" {
		return fmt.Errorf("--title is required")
	}
	if assignee == "" {
		return fmt.Errorf("--assignee is required")
	}

	// Resolve assignee by name — check agents first, then members.
	assigneeType, assigneeID, err := resolveAssigneeForSchedule(c, wsID, assignee)
	if err != nil {
		return fmt.Errorf("resolve assignee: %w", err)
	}

	body := map[string]any{
		"name":            name,
		"title_template":  title,
		"description":     desc,
		"assignee_type":   assigneeType,
		"assignee_id":     assigneeID,
		"priority":        priority,
		"cron_expression": cron,
		"timezone":        tz,
		"enabled":         enabled,
	}

	var row scheduleRow
	if err := c.PostJSON(context.Background(), "/api/schedules", body, &row); err != nil {
		return err
	}
	format, _ := cmd.Flags().GetString("output")
	if format == "json" {
		return cli.PrintJSON(os.Stdout, row)
	}
	cli.PrintTable(os.Stdout,
		[]string{"ID", "NAME", "CRON", "NEXT_RUN"},
		[][]string{{shortID(row.ID), row.Name, row.CronExpression, formatTime(row.NextRunAt)}},
	)
	return nil
}

func runScheduleUpdate(cmd *cobra.Command, args []string) error {
	c, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	wsID := resolveWorkspaceID(cmd)
	body := map[string]any{}
	if v, _ := cmd.Flags().GetString("name"); v != "" {
		body["name"] = v
	}
	if v, _ := cmd.Flags().GetString("cron"); v != "" {
		body["cron_expression"] = v
	}
	if v, _ := cmd.Flags().GetString("timezone"); v != "" {
		body["timezone"] = v
	}
	if v, _ := cmd.Flags().GetString("title"); v != "" {
		body["title_template"] = v
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("priority"); v != "" {
		body["priority"] = v
	}
	if v, _ := cmd.Flags().GetString("enabled"); v != "" {
		body["enabled"] = v == "true"
	}
	if v, _ := cmd.Flags().GetString("assignee"); v != "" {
		aType, aID, err := resolveAssigneeForSchedule(c, wsID, v)
		if err != nil {
			return fmt.Errorf("resolve assignee: %w", err)
		}
		body["assignee_type"] = aType
		body["assignee_id"] = aID
	}
	if len(body) == 0 {
		return fmt.Errorf("nothing to update — specify at least one flag")
	}

	var row scheduleRow
	if err := c.PatchJSON(context.Background(), "/api/schedules/"+args[0], body, &row); err != nil {
		return err
	}
	format, _ := cmd.Flags().GetString("output")
	if format == "json" {
		return cli.PrintJSON(os.Stdout, row)
	}
	fmt.Println("Updated.")
	return nil
}

func runScheduleDelete(cmd *cobra.Command, args []string) error {
	c, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if err := c.DeleteJSON(context.Background(), "/api/schedules/"+args[0]); err != nil {
		return err
	}
	fmt.Println("Deleted.")
	return nil
}

func runScheduleRun(cmd *cobra.Command, args []string) error {
	c, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var result map[string]any
	if err := c.PostJSON(context.Background(), "/api/schedules/"+args[0]+"/run", nil, &result); err != nil {
		return err
	}
	format, _ := cmd.Flags().GetString("output")
	if format == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Fired. Issue: %v\n", result["issue_id"])
	return nil
}

// resolveAssigneeForSchedule maps a human-readable name to (type, id).
// It checks agents first, then members.
func resolveAssigneeForSchedule(c *cli.APIClient, wsID, name string) (string, string, error) {
	// Try agents
	var agents []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := c.GetJSON(context.Background(), "/api/agents", &agents); err == nil {
		var matches []struct{ ID, Name string }
		for _, a := range agents {
			if strings.EqualFold(a.Name, name) {
				matches = append(matches, struct{ ID, Name string }{a.ID, a.Name})
			}
		}
		if len(matches) == 1 {
			return "agent", matches[0].ID, nil
		}
		if len(matches) > 1 {
			return "", "", fmt.Errorf("ambiguous agent name %q, matches %d agents", name, len(matches))
		}
	}

	// Try members
	var members []struct {
		ID   string `json:"id"`
		User struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	if err := c.GetJSON(context.Background(), "/api/workspaces/"+wsID+"/members", &members); err == nil {
		for _, m := range members {
			if strings.EqualFold(m.User.Name, name) {
				return "member", m.ID, nil
			}
		}
	}

	return "", "", fmt.Errorf("no agent or member found matching %q", name)
}

func formatTime(s string) string {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return s
	}
	return t.Format("2006-01-02 15:04")
}

func shortID(id string) string {
	if len(id) >= 8 {
		return id[:8]
	}
	return id
}
