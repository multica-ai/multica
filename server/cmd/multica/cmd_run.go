package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/cli/prompt"
)

// runCmd starts a coding agent on a local path, assigning it either an
// existing issue or an auto-created one. Flags not supplied on the command
// line are collected via interactive prompts with fuzzy filtering.
//
// Typical usage:
//
//	multica run                         # fully interactive
//	multica run --path . "fix auth bug" # auto-create issue from prompt
//	multica run --issue ABC-123         # run on an existing issue
var runCmd = &cobra.Command{
	Use:   "run [prompt...]",
	Short: "Start a coding agent on a local repo",
	Long: `Start a coding agent on a local git checkout, assigning it an issue.

The command links a local path to your workspace as a "local" repository (if
it isn't already), resolves or creates an issue, picks a project and agent,
then assigns the issue so the daemon picks it up.

Any flag you omit is collected via a one-at-a-time interactive prompt with
fuzzy search. Pass --yes to error out instead of prompting — useful in CI.

Examples:
  # Fully interactive — prompts walk you through path, project, agent, issue.
  multica run

  # Auto-create an issue from the prompt; use current directory as the repo.
  multica run --path . "refactor the auth middleware to use JWT"

  # Run on an existing issue, explicit agent.
  multica run --path ~/code/foo --issue a1b2c3d4 --agent Lambda`,
	RunE: runRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.GroupID = groupCore

	runCmd.Flags().String("path", "", "Local path of the git repo (default: current directory if it's a git repo; prompts otherwise)")
	runCmd.Flags().String("issue", "", "Existing issue id to work on")
	runCmd.Flags().Bool("autocreate", false, "Auto-create issue from prompt (default when no --issue)")
	runCmd.Flags().String("project", "", "Project name or id")
	runCmd.Flags().String("agent", "", "Agent name or id")
	runCmd.Flags().String("title", "", "Override auto-generated issue title")
	runCmd.Flags().Bool("yes", false, "Non-interactive: fail instead of prompting")
}

func runRun(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		return fmt.Errorf("workspace ID is required; use --workspace-id or set MULTICA_WORKSPACE_ID")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	yesFlag, _ := cmd.Flags().GetBool("yes")
	promptText := strings.TrimSpace(strings.Join(args, " "))

	// -------------------------------------------------------------------
	// 1. Resolve local path
	// -------------------------------------------------------------------
	path, _ := cmd.Flags().GetString("path")
	path, err = resolveRunPath(path, yesFlag)
	if err != nil {
		return err
	}
	if err := validateGitRepo(path); err != nil {
		return err
	}

	// -------------------------------------------------------------------
	// 2. Ensure workspace.repos contains this local path
	// -------------------------------------------------------------------
	repo, err := ensureLocalRepoInWorkspace(ctx, client, path)
	if err != nil {
		return fmt.Errorf("register local repo: %w", err)
	}
	fmt.Fprintf(os.Stderr, "✓ repo: %s (%s)\n", repo.Name, repo.LocalPath)

	// -------------------------------------------------------------------
	// 3. Resolve project (optional)
	// -------------------------------------------------------------------
	projectID, err := resolveProject(ctx, client, cmd, yesFlag)
	if err != nil {
		return err
	}
	// Link the repo to the chosen project so the daemon scoping works.
	if projectID != "" {
		if err := linkRepoToProject(ctx, client, projectID, repo.ID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not link repo to project: %v\n", err)
		}
	}

	// -------------------------------------------------------------------
	// 4. Resolve agent
	// -------------------------------------------------------------------
	agentID, agentName, err := resolveAgent(ctx, client, cmd, yesFlag)
	if err != nil {
		return err
	}

	// -------------------------------------------------------------------
	// 5. Resolve or create issue
	// -------------------------------------------------------------------
	issueID, issueTitle, err := resolveIssue(ctx, client, cmd, projectID, promptText, yesFlag)
	if err != nil {
		return err
	}

	// -------------------------------------------------------------------
	// 6. Assign the issue to the agent (this enqueues the task)
	// -------------------------------------------------------------------
	assignBody := map[string]any{
		"assignee_type": "agent",
		"assignee_id":   agentID,
	}
	var assigned map[string]any
	if err := client.PutJSON(ctx, "/api/issues/"+issueID, assignBody, &assigned); err != nil {
		return fmt.Errorf("assign issue: %w", err)
	}

	// -------------------------------------------------------------------
	// 7. Summary
	// -------------------------------------------------------------------
	fmt.Fprintf(os.Stderr, "\n✓ Issue %q assigned to %s\n", issueTitle, agentName)
	fmt.Fprintf(os.Stderr, "  Follow progress: multica issue runs %s\n", issueID)
	return nil
}

// -----------------------------------------------------------------------------
// Path resolution
// -----------------------------------------------------------------------------

func resolveRunPath(flagVal string, yes bool) (string, error) {
	if flagVal != "" {
		return normalizePath(flagVal)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve current directory: %w", err)
	}
	// Non-interactive: use cwd unconditionally. validateGitRepo runs after
	// this and will return a clear error if cwd isn't a git repo.
	if yes || !prompt.IsInteractive() {
		return cwd, nil
	}
	// Interactive: if the current directory is already a git repo, just use
	// it silently — prompting the user to confirm their own cwd is noise.
	// Only prompt when we have nothing sensible to default to.
	if isGitRepo(cwd) {
		fmt.Fprintf(os.Stderr, "Using current directory as repo path: %s\n", cwd)
		return cwd, nil
	}
	val, err := prompt.Input("Local repository path", "/absolute/path/to/repo", "")
	if err != nil {
		return "", err
	}
	if val == "" {
		return "", fmt.Errorf("path is required")
	}
	return normalizePath(val)
}

func normalizePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		if p == "~" {
			p = home
		} else if strings.HasPrefix(p, "~/") {
			p = filepath.Join(home, p[2:])
		}
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	return abs, nil
}

func validateGitRepo(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("path %s does not exist: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %s is not a directory", path)
	}
	if !isGitRepo(path) {
		return fmt.Errorf("path %s is not a git repository (run `git init` first or point at an existing checkout)", path)
	}
	return nil
}

func isGitRepo(path string) bool {
	out, err := exec.Command("git", "-C", path, "rev-parse", "--git-dir").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// -----------------------------------------------------------------------------
// Repo registration in workspace.repos
// -----------------------------------------------------------------------------

type workspaceRepoEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	URL         string `json:"url,omitempty"`
	LocalPath   string `json:"local_path,omitempty"`
	Description string `json:"description"`
}

// ensureLocalRepoInWorkspace looks for a local repo entry matching path. If
// absent, it PATCHes the workspace with a new entry. Returns the entry.
func ensureLocalRepoInWorkspace(ctx context.Context, client *cli.APIClient, absPath string) (*workspaceRepoEntry, error) {
	var ws map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+client.WorkspaceID, &ws); err != nil {
		return nil, fmt.Errorf("load workspace: %w", err)
	}
	rawRepos, _ := ws["repos"].([]any)
	existing := make([]workspaceRepoEntry, 0, len(rawRepos))
	for _, r := range rawRepos {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		existing = append(existing, workspaceRepoEntry{
			ID:          strVal(m, "id"),
			Name:        strVal(m, "name"),
			Type:        strVal(m, "type"),
			URL:         strVal(m, "url"),
			LocalPath:   strVal(m, "local_path"),
			Description: strVal(m, "description"),
		})
	}

	for _, r := range existing {
		if r.Type == "local" && r.LocalPath == absPath {
			e := r
			return &e, nil
		}
	}

	// Not found — create a new entry.
	newEntry := workspaceRepoEntry{
		ID:        uuid.NewString(),
		Name:      filepath.Base(absPath),
		Type:      "local",
		LocalPath: absPath,
	}
	updated := append(existing, newEntry)
	var _resp map[string]any
	if err := client.PutJSON(ctx, "/api/workspaces/"+client.WorkspaceID, map[string]any{
		"repos": updated,
	}, &_resp); err != nil {
		return nil, fmt.Errorf("update workspace repos: %w", err)
	}
	return &newEntry, nil
}

// -----------------------------------------------------------------------------
// Project resolution
// -----------------------------------------------------------------------------

func resolveProject(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, yes bool) (string, error) {
	flagVal, _ := cmd.Flags().GetString("project")
	projects, err := listProjectsForRun(ctx, client)
	if err != nil {
		return "", err
	}
	if flagVal != "" {
		for _, p := range projects {
			if strings.EqualFold(p.ID, flagVal) || strings.EqualFold(p.Title, flagVal) || strings.HasPrefix(p.ID, flagVal) {
				return p.ID, nil
			}
		}
		return "", fmt.Errorf("no project matches %q (run `multica project list`)", flagVal)
	}
	if yes || !prompt.IsInteractive() {
		return "", nil // project is optional; non-interactive skips
	}
	opts := make([]prompt.Option[string], 0, len(projects)+1)
	opts = append(opts, prompt.Option[string]{Label: "[No project]", Value: ""})
	for _, p := range projects {
		opts = append(opts, prompt.Option[string]{
			Label:       p.Title,
			Description: p.Status,
			Value:       p.ID,
		})
	}
	chosen, err := prompt.Select("Project?", opts)
	if err != nil {
		return "", err
	}
	return chosen, nil
}

type runProject struct {
	ID     string
	Title  string
	Status string
}

func listProjectsForRun(ctx context.Context, client *cli.APIClient) ([]runProject, error) {
	path := "/api/projects?workspace_id=" + url.QueryEscape(client.WorkspaceID)
	var resp map[string]any
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	raw, _ := resp["projects"].([]any)
	out := make([]runProject, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, runProject{
			ID:     strVal(m, "id"),
			Title:  strVal(m, "title"),
			Status: strVal(m, "status"),
		})
	}
	return out, nil
}

func linkRepoToProject(ctx context.Context, client *cli.APIClient, projectID, repoID string) error {
	// Fetch current repo links, append if missing, then PUT.
	var project map[string]any
	if err := client.GetJSON(ctx, "/api/projects/"+projectID, &project); err != nil {
		return err
	}
	rawIDs, _ := project["repo_ids"].([]any)
	ids := make([]string, 0, len(rawIDs)+1)
	for _, v := range rawIDs {
		if s, ok := v.(string); ok {
			if s == repoID {
				return nil // already linked
			}
			ids = append(ids, s)
		}
	}
	ids = append(ids, repoID)
	var _resp map[string]any
	return client.PutJSON(ctx, "/api/projects/"+projectID, map[string]any{"repo_ids": ids}, &_resp)
}

// -----------------------------------------------------------------------------
// Agent resolution
// -----------------------------------------------------------------------------

type runAgent struct {
	ID   string
	Name string
}

func resolveAgent(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, yes bool) (string, string, error) {
	flagVal, _ := cmd.Flags().GetString("agent")
	agents, err := listAgentsForRun(ctx, client)
	if err != nil {
		return "", "", err
	}
	if len(agents) == 0 {
		return "", "", fmt.Errorf("no agents available in this workspace (create one at /settings/agents)")
	}
	if flagVal != "" {
		for _, a := range agents {
			if strings.EqualFold(a.ID, flagVal) || strings.EqualFold(a.Name, flagVal) || strings.HasPrefix(a.ID, flagVal) {
				return a.ID, a.Name, nil
			}
		}
		return "", "", fmt.Errorf("no agent matches %q", flagVal)
	}
	if yes || !prompt.IsInteractive() {
		return "", "", fmt.Errorf("--agent is required when non-interactive")
	}
	opts := make([]prompt.Option[runAgent], len(agents))
	for i, a := range agents {
		opts[i] = prompt.Option[runAgent]{Label: a.Name, Value: a}
	}
	chosen, err := prompt.Select("Agent?", opts)
	if err != nil {
		return "", "", err
	}
	return chosen.ID, chosen.Name, nil
}

func listAgentsForRun(ctx context.Context, client *cli.APIClient) ([]runAgent, error) {
	path := "/api/agents?workspace_id=" + url.QueryEscape(client.WorkspaceID)
	var agents []map[string]any
	if err := client.GetJSON(ctx, path, &agents); err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	out := make([]runAgent, 0, len(agents))
	for _, a := range agents {
		out = append(out, runAgent{
			ID:   strVal(a, "id"),
			Name: strVal(a, "name"),
		})
	}
	return out, nil
}

// -----------------------------------------------------------------------------
// Issue resolution / auto-creation
// -----------------------------------------------------------------------------

const sentinelAutocreate = "__autocreate__"

func resolveIssue(ctx context.Context, client *cli.APIClient, cmd *cobra.Command, projectID, promptText string, yes bool) (string, string, error) {
	issueFlag, _ := cmd.Flags().GetString("issue")
	autocreateFlag, _ := cmd.Flags().GetBool("autocreate")
	titleFlag, _ := cmd.Flags().GetString("title")

	if issueFlag != "" {
		// Look up to confirm it exists and read its title.
		var iss map[string]any
		if err := client.GetJSON(ctx, "/api/issues/"+issueFlag, &iss); err != nil {
			return "", "", fmt.Errorf("load issue %s: %w", issueFlag, err)
		}
		return strVal(iss, "id"), strVal(iss, "title"), nil
	}
	if autocreateFlag || yes || !prompt.IsInteractive() {
		return createIssueForRun(ctx, client, projectID, promptText, titleFlag)
	}

	// Interactive picker: autocreate is the first (default) option.
	issues, err := listIssuesForRun(ctx, client, projectID)
	if err != nil {
		return "", "", err
	}
	opts := make([]prompt.Option[string], 0, len(issues)+1)
	opts = append(opts, prompt.Option[string]{
		Label:       "[Autocreate issue from prompt]",
		Description: firstLine(promptText),
		Value:       sentinelAutocreate,
	})
	for _, i := range issues {
		label := i.Title
		if i.Number > 0 {
			label = fmt.Sprintf("#%d %s", i.Number, i.Title)
		}
		opts = append(opts, prompt.Option[string]{
			Label:       label,
			Description: i.Status,
			Value:       i.ID,
		})
	}
	chosen, err := prompt.Select("Which issue?", opts)
	if err != nil {
		return "", "", err
	}
	if chosen == sentinelAutocreate {
		return createIssueForRun(ctx, client, projectID, promptText, titleFlag)
	}
	for _, i := range issues {
		if i.ID == chosen {
			return i.ID, i.Title, nil
		}
	}
	return chosen, "", nil
}

type runIssue struct {
	ID     string
	Title  string
	Status string
	Number int
}

func listIssuesForRun(ctx context.Context, client *cli.APIClient, projectID string) ([]runIssue, error) {
	params := url.Values{}
	params.Set("workspace_id", client.WorkspaceID)
	if projectID != "" {
		params.Set("project_id", projectID)
	}
	params.Set("limit", "50")
	var resp map[string]any
	if err := client.GetJSON(ctx, "/api/issues?"+params.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	raw, _ := resp["issues"].([]any)
	out := make([]runIssue, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		num := 0
		if n, ok := m["number"].(float64); ok {
			num = int(n)
		}
		out = append(out, runIssue{
			ID:     strVal(m, "id"),
			Title:  strVal(m, "title"),
			Status: strVal(m, "status"),
			Number: num,
		})
	}
	return out, nil
}

func createIssueForRun(ctx context.Context, client *cli.APIClient, projectID, promptText, titleOverride string) (string, string, error) {
	title, description := extractTitleAndBody(promptText, titleOverride)
	if title == "" {
		if prompt.IsInteractive() {
			t, err := prompt.Input("Issue title", "", "")
			if err != nil {
				return "", "", err
			}
			title = t
		}
	}
	if title == "" {
		return "", "", fmt.Errorf("issue title is empty; provide a prompt or --title")
	}
	body := map[string]any{
		"title":       title,
		"description": description,
	}
	if projectID != "" {
		body["project_id"] = projectID
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/issues", body, &result); err != nil {
		return "", "", fmt.Errorf("create issue: %w", err)
	}
	return strVal(result, "id"), title, nil
}

// extractTitleAndBody derives (title, description) from a free-form prompt.
//
// Rules:
//   - Title is always a short summary (capped at maxTitleLen) — never the
//     full prompt. A long single-line prompt becomes a truncated title
//     ending in "…", never an 80-character run-on.
//   - Description always contains the full, original prompt so the agent
//     (and anyone reading the issue in the UI) sees the whole ask. The only
//     exception is a short single-line prompt where title == prompt; then
//     the description is empty to avoid duplication.
//   - titleOverride always wins; the entire promptText becomes the body.
//   - Leading blank lines are skipped when picking the first "line".
func extractTitleAndBody(promptText, titleOverride string) (string, string) {
	promptText = strings.TrimSpace(promptText)
	if titleOverride != "" {
		return titleOverride, promptText
	}
	if promptText == "" {
		return "", ""
	}

	// First non-blank line is our title candidate.
	firstLineRaw := promptText
	if nl := strings.IndexByte(promptText, '\n'); nl >= 0 {
		firstLineRaw = strings.TrimSpace(promptText[:nl])
		// Skip leading blank lines.
		rest := strings.TrimSpace(promptText[nl+1:])
		for firstLineRaw == "" && rest != "" {
			nl = strings.IndexByte(rest, '\n')
			if nl < 0 {
				firstLineRaw = rest
				rest = ""
				break
			}
			firstLineRaw = strings.TrimSpace(rest[:nl])
			rest = strings.TrimSpace(rest[nl+1:])
		}
	}

	title := truncateTitle(firstLineRaw)

	// Description rules:
	//   - single-line prompt that wasn't truncated → no description (title
	//     already says everything)
	//   - anything else (multi-line, or truncated title) → full original
	//     prompt as the description so no content is lost
	isSingleLine := !strings.ContainsRune(promptText, '\n')
	if isSingleLine && title == firstLineRaw {
		return title, ""
	}
	return title, promptText
}

// maxTitleLen caps the issue title. Chosen short enough that the title fits
// in one line of the board and the issue list without visually dominating
// sibling rows, but long enough to meaningfully describe a task.
const maxTitleLen = 60

func truncateTitle(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxTitleLen {
		return s
	}
	// Trim on a word boundary if possible, leaving room for "…".
	cut := s[:maxTitleLen]
	if i := strings.LastIndexByte(cut, ' '); i > maxTitleLen/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		return strings.TrimSpace(s[:nl])
	}
	return s
}

// -----------------------------------------------------------------------------
// Misc
// -----------------------------------------------------------------------------

// errSilent is used elsewhere for quiet cancellation; reuse where applicable.
var _ = errors.New
