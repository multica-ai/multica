package main

import (
	"context"
	"encoding/json"
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

var projectRepoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage project repositories",
}

var projectRepoListCmd = &cobra.Command{
	Use:   "list <project-id>",
	Short: "List repos linked to a project",
	Args:  exactArgs(1),
	RunE:  runProjectRepoList,
}

var projectRepoAddCmd = &cobra.Command{
	Use:   "add <project-id> [url]",
	Short: "Add a repository to a project (use --path for local repos)",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runProjectRepoAdd,
}

var projectRepoRemoveCmd = &cobra.Command{
	Use:   "remove <project-id> <url-or-path>",
	Short: "Remove a repository from a project",
	Args:  exactArgs(2),
	RunE:  runProjectRepoRemove,
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
	projectCmd.AddCommand(projectRepoCmd)
	projectRepoCmd.AddCommand(projectRepoListCmd)
	projectRepoCmd.AddCommand(projectRepoAddCmd)
	projectRepoCmd.AddCommand(projectRepoRemoveCmd)

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
	projectCreateCmd.Flags().StringArray("repo", nil, "Repository URL or local path to link (repeatable)")
	projectCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// project update
	projectUpdateCmd.Flags().String("title", "", "New title")
	projectUpdateCmd.Flags().String("description", "", "New description")
	projectUpdateCmd.Flags().String("status", "", "New status")
	projectUpdateCmd.Flags().String("icon", "", "New icon (emoji)")
	projectUpdateCmd.Flags().String("lead", "", "New lead name (member or agent)")
	projectUpdateCmd.Flags().StringArray("repo", nil, "Set repositories linked to this project (URL or local path, repeatable); replaces existing list")
	projectUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// project delete
	projectDeleteCmd.Flags().String("output", "json", "Output format: table or json")

	// project status
	projectStatusCmd.Flags().String("output", "table", "Output format: table or json")

	// project repo list
	projectRepoListCmd.Flags().String("output", "table", "Output format: table or json")

	// project repo add
	projectRepoAddCmd.Flags().String("path", "", "Absolute local path to the repository (alternative to url positional arg)")
	projectRepoAddCmd.Flags().String("source-branch", "", "Branch to check out when working on this repo (optional)")
	projectRepoAddCmd.Flags().String("target-branch", "", "Branch to commit all work to in this repo (optional)")

	// project repo remove
	// no extra flags needed
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
	if repos, _ := cmd.Flags().GetStringArray("repo"); len(repos) > 0 {
		repoObjs, err := resolveRepoIdentifiers(ctx, client, repos)
		if err != nil {
			return err
		}
		body["repos"] = repoObjs
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
	if cmd.Flags().Changed("repo") {
		repos, _ := cmd.Flags().GetStringArray("repo")
		repoObjs, err := resolveRepoIdentifiers(ctx, client, repos)
		if err != nil {
			return err
		}
		if repoObjs == nil {
			repoObjs = []map[string]any{}
		}
		body["repos"] = repoObjs
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --status, --description, --icon, --lead, --repo")
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
// Project repo commands
// ---------------------------------------------------------------------------

func runProjectRepoList(cmd *cobra.Command, args []string) error {
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

	repos, _ := project["repos"].([]any)
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, repos)
	}

	headers := []string{"URL", "LOCAL PATH", "DESCRIPTION"}
	rows := make([][]string, 0, len(repos))
	for _, raw := range repos {
		r, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			strVal(r, "url"),
			strVal(r, "local_path"),
			strVal(r, "description"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runProjectRepoAdd(cmd *cobra.Command, args []string) error {
	projectID := args[0]
	localPath, _ := cmd.Flags().GetString("path")
	sourceBranch, _ := cmd.Flags().GetString("source-branch")
	targetBranch, _ := cmd.Flags().GetString("target-branch")

	var repoURL string
	if len(args) >= 2 {
		repoURL = args[1]
	}

	if repoURL == "" && localPath == "" {
		return fmt.Errorf("provide a URL (positional arg) or a local path (--path)")
	}
	if repoURL != "" && localPath != "" {
		return fmt.Errorf("--path and url are mutually exclusive")
	}

	// identifier used for lookups and display
	identifier := repoURL
	if localPath != "" {
		identifier = localPath
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var ws map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+client.WorkspaceID, &ws); err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}
	wsRepos, _ := ws["repos"].([]any)
	var wsDesc string
	foundInWs := false
	for _, raw := range wsRepos {
		r, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		wsKey := strVal(r, "local_path")
		if wsKey == "" {
			wsKey = strVal(r, "url")
		}
		if wsKey == identifier {
			foundInWs = true
			wsDesc = strVal(r, "description")
			break
		}
	}
	if !foundInWs {
		return fmt.Errorf("repository %s is not configured in this workspace — add it in Settings → Repositories first", identifier)
	}

	var project map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectID, &project); err != nil {
		return fmt.Errorf("get project: %w", err)
	}

	repos, _ := project["repos"].([]any)
	for _, raw := range repos {
		r, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		existing := strVal(r, "local_path")
		if existing == "" {
			existing = strVal(r, "url")
		}
		if existing == identifier {
			return fmt.Errorf("repository %s already linked to this project", identifier)
		}
	}

	var newRepo map[string]any
	if localPath != "" {
		newRepo = map[string]any{"local_path": localPath, "description": wsDesc}
	} else {
		newRepo = map[string]any{"url": repoURL, "description": wsDesc}
	}
	if sourceBranch != "" {
		newRepo["source_branch"] = sourceBranch
	}
	if targetBranch != "" {
		newRepo["target_branch"] = targetBranch
	}
	repos = append(repos, newRepo)

	body := map[string]any{"repos": repos}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+projectID, body, &result); err != nil {
		return fmt.Errorf("update project repos: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Repository %s added to project %s.\n", identifier, truncateID(projectID))
	return nil
}

func runProjectRepoRemove(cmd *cobra.Command, args []string) error {
	projectID := args[0]
	identifier := args[1] // URL or local path

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var project map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectID, &project); err != nil {
		return fmt.Errorf("get project: %w", err)
	}

	repos, _ := project["repos"].([]any)
	filtered := make([]any, 0, len(repos))
	found := false
	for _, raw := range repos {
		r, ok := raw.(map[string]any)
		if !ok {
			filtered = append(filtered, raw)
			continue
		}
		key := strVal(r, "local_path")
		if key == "" {
			key = strVal(r, "url")
		}
		if key == identifier {
			found = true
			continue
		}
		filtered = append(filtered, raw)
	}
	if !found {
		return fmt.Errorf("repository %s not found in this project", identifier)
	}

	body := map[string]any{"repos": filtered}
	var result map[string]any
	if err := client.PutJSON(ctx, "/api/projects/"+projectID, body, &result); err != nil {
		return fmt.Errorf("update project repos: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Repository %s removed from project %s.\n", identifier, truncateID(projectID))
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// repoIdentifierInput represents a parsed --repo flag value.
type repoIdentifierInput struct {
	Identifier   string // URL or local path
	SourceBranch string
	TargetBranch string
	MachinePaths map[string]string // device_name -> local path override
}

// parseRepoInput parses a --repo flag value. Supports two formats:
//
//	"https://github.com/foo/bar"                   — plain identifier (backward-compatible)
//	'{"url":"...","source_branch":"...","target_branch":"...","machine_paths":{"host":"/path"}}'  — JSON with optional branches and per-machine paths
func parseRepoInput(s string) (repoIdentifierInput, error) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "{") {
		var parsed struct {
			URL          string            `json:"url"`
			LocalPath    string            `json:"local_path"`
			SourceBranch string            `json:"source_branch"`
			TargetBranch string            `json:"target_branch"`
			MachinePaths map[string]string `json:"machine_paths"`
		}
		if err := json.Unmarshal([]byte(s), &parsed); err != nil {
			return repoIdentifierInput{}, fmt.Errorf("invalid JSON repo format: %w", err)
		}
		id := parsed.LocalPath
		if id == "" {
			id = parsed.URL
		}
		if id == "" {
			return repoIdentifierInput{}, fmt.Errorf("JSON repo must have a 'url' or 'local_path' field")
		}
		return repoIdentifierInput{Identifier: id, SourceBranch: parsed.SourceBranch, TargetBranch: parsed.TargetBranch, MachinePaths: parsed.MachinePaths}, nil
	}
	return repoIdentifierInput{Identifier: s}, nil
}

// resolveRepoIdentifiers resolves a list of URL/local-path strings (or JSON objects)
// against the workspace repo registry and returns the repo objects ready for the API payload.
func resolveRepoIdentifiers(ctx context.Context, client *cli.APIClient, identifiers []string) ([]map[string]any, error) {
	var ws map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+client.WorkspaceID, &ws); err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	wsRepos, _ := ws["repos"].([]any)
	wsRepoMap := make(map[string]map[string]any, len(wsRepos))
	for _, raw := range wsRepos {
		r, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key := strVal(r, "local_path")
		if key != "" {
			wsRepoMap[key] = r
		}
		if url := strVal(r, "url"); url != "" {
			wsRepoMap[url] = r
		}
	}
	result := make([]map[string]any, 0, len(identifiers))
	for _, raw := range identifiers {
		input, err := parseRepoInput(raw)
		if err != nil {
			return nil, err
		}
		r, ok := wsRepoMap[input.Identifier]
		if !ok {
			return nil, fmt.Errorf("repository %s is not configured in this workspace — add it in Settings → Repositories first", input.Identifier)
		}
		entry := map[string]any{"description": strVal(r, "description")}
		if lp := strVal(r, "local_path"); lp != "" {
			entry["local_path"] = lp
		}
		if url := strVal(r, "url"); url != "" {
			entry["url"] = url
		}
		if input.SourceBranch != "" {
			entry["source_branch"] = input.SourceBranch
		}
		if input.TargetBranch != "" {
			entry["target_branch"] = input.TargetBranch
		}
		if len(input.MachinePaths) > 0 {
			entry["machine_paths"] = input.MachinePaths
		}
		result = append(result, entry)
	}
	return result, nil
}

func formatLead(project map[string]any) string {
	lType := strVal(project, "lead_type")
	lID := strVal(project, "lead_id")
	if lType == "" || lID == "" {
		return ""
	}
	return lType + ":" + truncateID(lID)
}
