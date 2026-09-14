package main

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var projectPlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Author project plans",
}

var projectPlanCreateFromIssueCmd = &cobra.Command{
	Use:   "create-from-issue <project-id> <source-issue-id>",
	Short: "Create a plan by snapshotting a source issue",
	Args:  exactArgs(2),
	RunE:  runProjectPlanCreateFromIssue,
}

var projectPlanAddPhaseCmd = &cobra.Command{
	Use:   "add-phase <project-id> <plan-id>",
	Short: "Add a phase to a project plan",
	Args:  exactArgs(2),
	RunE:  runProjectPlanAddPhase,
}

var projectPlanAddPartCmd = &cobra.Command{
	Use:   "add-part <project-id> <plan-id> <phase-id>",
	Short: "Add a part to a project plan phase",
	Args:  exactArgs(3),
	RunE:  runProjectPlanAddPart,
}

var projectPlanLinkIssueCmd = &cobra.Command{
	Use:   "link-issue <project-id> <plan-id> <part-id> <issue-id>",
	Short: "Link an issue to a project plan part",
	Args:  exactArgs(4),
	RunE:  runProjectPlanLinkIssue,
}

func init() {
	projectCmd.AddCommand(projectPlanCmd)
	projectPlanCmd.AddCommand(projectPlanCreateFromIssueCmd)
	projectPlanCmd.AddCommand(projectPlanAddPhaseCmd)
	projectPlanCmd.AddCommand(projectPlanAddPartCmd)
	projectPlanCmd.AddCommand(projectPlanLinkIssueCmd)

	projectPlanCreateFromIssueCmd.Flags().String("kind", "prd", "Plan kind")
	projectPlanCreateFromIssueCmd.Flags().String("output", "json", "Output format: table or json")

	projectPlanAddPhaseCmd.Flags().String("title", "", "Phase title (required)")
	projectPlanAddPhaseCmd.Flags().String("description", "", "Phase description")
	projectPlanAddPhaseCmd.Flags().Int32("position", 0, "Phase position (zero-based)")
	projectPlanAddPhaseCmd.Flags().String("output", "json", "Output format: table or json")

	projectPlanAddPartCmd.Flags().String("title", "", "Part title (required)")
	projectPlanAddPartCmd.Flags().String("description", "", "Part description")
	projectPlanAddPartCmd.Flags().String("acceptance-criteria", "", "Part acceptance criteria")
	projectPlanAddPartCmd.Flags().Int32("position", 0, "Part position within the phase (zero-based)")
	projectPlanAddPartCmd.Flags().String("output", "json", "Output format: table or json")

	projectPlanLinkIssueCmd.Flags().String("output", "json", "Output format: table or json")
}

func runProjectPlanCreateFromIssue(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	project, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	source, err := resolveIssueRef(ctx, client, args[1])
	if err != nil {
		return fmt.Errorf("resolve source issue: %w", err)
	}
	kind, _ := cmd.Flags().GetString("kind")
	body := map[string]any{"source_issue_id": source.ID, "kind": kind}

	var result map[string]any
	path := "/api/projects/" + url.PathEscape(project.ID) + "/plans/from-issue"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("create project plan from issue: %w", err)
	}
	return printProjectPlanResult(cmd, result, []string{"id", "title", "origin"})
}

func runProjectPlanAddPhase(cmd *cobra.Command, args []string) error {
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

	project, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	description, _ := cmd.Flags().GetString("description")
	position, _ := cmd.Flags().GetInt32("position")
	body := map[string]any{"title": title, "description": description, "position": position}

	var result map[string]any
	path := "/api/projects/" + url.PathEscape(project.ID) + "/plans/" + url.PathEscape(args[1]) + "/phases"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("add project plan phase: %w", err)
	}
	return printProjectPlanResult(cmd, result, []string{"id", "title", "position"})
}

func runProjectPlanAddPart(cmd *cobra.Command, args []string) error {
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

	project, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	description, _ := cmd.Flags().GetString("description")
	acceptanceCriteria, _ := cmd.Flags().GetString("acceptance-criteria")
	position, _ := cmd.Flags().GetInt32("position")
	body := map[string]any{
		"title": title, "description": description,
		"acceptance_criteria": acceptanceCriteria, "position": position,
	}

	var result map[string]any
	path := "/api/projects/" + url.PathEscape(project.ID) + "/plans/" + url.PathEscape(args[1]) +
		"/phases/" + url.PathEscape(args[2]) + "/parts"
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("add project plan part: %w", err)
	}
	return printProjectPlanResult(cmd, result, []string{"id", "title", "position"})
}

func runProjectPlanLinkIssue(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	project, err := resolveProjectID(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve project: %w", err)
	}
	issue, err := resolveIssueRef(ctx, client, args[3])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	path := "/api/projects/" + url.PathEscape(project.ID) + "/plans/" + url.PathEscape(args[1]) +
		"/parts/" + url.PathEscape(args[2]) + "/issues/" + url.PathEscape(issue.ID)

	var result map[string]any
	if err := client.PostJSON(ctx, path, map[string]any{}, &result); err != nil {
		return fmt.Errorf("link issue to project plan part: %w", err)
	}
	return printProjectPlanResult(cmd, result, []string{"id", "project_plan_part_id", "issue_id"})
}

func printProjectPlanResult(cmd *cobra.Command, result map[string]any, fields []string) error {
	output, _ := cmd.Flags().GetString("output")
	if output != "table" {
		return cli.PrintJSON(os.Stdout, result)
	}
	headers := make([]string, len(fields))
	row := make([]string, len(fields))
	for i, field := range fields {
		headers[i] = field
		row[i] = fmt.Sprint(result[field])
	}
	cli.PrintTable(os.Stdout, headers, [][]string{row})
	return nil
}
