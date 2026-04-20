package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Work with workspaces",
}

var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workspaces you belong to",
	RunE:  runWorkspaceList,
}

var workspaceGetCmd = &cobra.Command{
	Use:   "get [workspace-id]",
	Short: "Get workspace details",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceGet,
}

var workspaceMembersCmd = &cobra.Command{
	Use:   "members [workspace-id]",
	Short: "List workspace members",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceMembers,
}

var workspaceWatchCmd = &cobra.Command{
	Use:   "watch <workspace-id>",
	Short: "Add a workspace to the daemon watch list",
	Args:  exactArgs(1),
	RunE:  runWatch,
}

var workspaceUnwatchCmd = &cobra.Command{
	Use:   "unwatch <workspace-id>",
	Short: "Remove a workspace from the daemon watch list",
	Args:  exactArgs(1),
	RunE:  runUnwatch,
}

var workspaceRepoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage repositories attached to the workspace",
}

var workspaceRepoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List repositories attached to the workspace",
	Args:  cobra.NoArgs,
	RunE:  runWorkspaceRepoList,
}

var workspaceRepoAddCmd = &cobra.Command{
	Use:   "add <url>",
	Short: "Add a repository to the workspace",
	Args:  exactArgs(1),
	RunE:  runWorkspaceRepoAdd,
}

var workspaceRepoRemoveCmd = &cobra.Command{
	Use:   "remove <url>",
	Short: "Remove a repository from the workspace",
	Args:  exactArgs(1),
	RunE:  runWorkspaceRepoRemove,
}

var workspaceRepoUpdateCmd = &cobra.Command{
	Use:   "update <url>",
	Short: "Update the description of an existing workspace repository",
	Args:  exactArgs(1),
	RunE:  runWorkspaceRepoUpdate,
}

func init() {
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceGetCmd)
	workspaceCmd.AddCommand(workspaceMembersCmd)
	workspaceCmd.AddCommand(workspaceWatchCmd)
	workspaceCmd.AddCommand(workspaceUnwatchCmd)
	workspaceCmd.AddCommand(workspaceRepoCmd)

	workspaceRepoCmd.AddCommand(workspaceRepoListCmd)
	workspaceRepoCmd.AddCommand(workspaceRepoAddCmd)
	workspaceRepoCmd.AddCommand(workspaceRepoRemoveCmd)
	workspaceRepoCmd.AddCommand(workspaceRepoUpdateCmd)

	workspaceGetCmd.Flags().String("output", "json", "Output format: table or json")
	workspaceMembersCmd.Flags().String("output", "table", "Output format: table or json")

	workspaceRepoListCmd.Flags().String("output", "table", "Output format: table or json")
	workspaceRepoAddCmd.Flags().String("description", "", "Description for the repository")
	workspaceRepoUpdateCmd.Flags().String("description", "", "New description for the repository (required)")
}

func runWorkspaceList(cmd *cobra.Command, _ []string) error {
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}

	client := cli.NewAPIClient(serverURL, "", token)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var workspaces []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return fmt.Errorf("list workspaces: %w", err)
	}

	if len(workspaces) == 0 {
		fmt.Fprintln(os.Stderr, "No workspaces found.")
		return nil
	}

	// Load watched set for marking.
	profile := resolveProfile(cmd)
	cfg, _ := cli.LoadCLIConfigForProfile(profile)
	watched := make(map[string]bool)
	for _, w := range cfg.WatchedWorkspaces {
		watched[w.ID] = true
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tWATCHING")
	for _, ws := range workspaces {
		mark := ""
		if watched[ws.ID] {
			mark = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", ws.ID, ws.Name, mark)
	}
	return w.Flush()
}

func workspaceIDFromArgs(cmd *cobra.Command, args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return resolveWorkspaceID(cmd)
}

func runWorkspaceGet(cmd *cobra.Command, args []string) error {
	wsID := workspaceIDFromArgs(cmd, args)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var ws map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &ws); err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		desc := strVal(ws, "description")
		if utf8.RuneCountInString(desc) > 60 {
			runes := []rune(desc)
			desc = string(runes[:57]) + "..."
		}
		wsContext := strVal(ws, "context")
		if utf8.RuneCountInString(wsContext) > 60 {
			runes := []rune(wsContext)
			wsContext = string(runes[:57]) + "..."
		}
		headers := []string{"ID", "NAME", "SLUG", "DESCRIPTION", "CONTEXT"}
		rows := [][]string{{
			strVal(ws, "id"),
			strVal(ws, "name"),
			strVal(ws, "slug"),
			desc,
			wsContext,
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, ws)
}

func runWorkspaceMembers(cmd *cobra.Command, args []string) error {
	wsID := workspaceIDFromArgs(cmd, args)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var members []map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID+"/members", &members); err != nil {
		return fmt.Errorf("list members: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, members)
	}

	headers := []string{"USER ID", "NAME", "EMAIL", "ROLE"}
	rows := make([][]string, 0, len(members))
	for _, m := range members {
		rows = append(rows, []string{
			strVal(m, "user_id"),
			strVal(m, "name"),
			strVal(m, "email"),
			strVal(m, "role"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runWatch(cmd *cobra.Command, args []string) error {
	workspaceID := args[0]

	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return fmt.Errorf("not authenticated: run 'multica login' first")
	}

	client := cli.NewAPIClient(serverURL, "", token)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var ws struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces/"+workspaceID, &ws); err != nil {
		return fmt.Errorf("workspace not found: %w", err)
	}

	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return err
	}

	if !cfg.AddWatchedWorkspace(ws.ID, ws.Name) {
		fmt.Fprintf(os.Stderr, "Already watching workspace %s (%s)\n", ws.ID, ws.Name)
		return nil
	}

	if cfg.WorkspaceID == "" {
		cfg.WorkspaceID = ws.ID
		fmt.Fprintf(os.Stderr, "Set default workspace to %s (%s)\n", ws.ID, ws.Name)
	}

	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Watching workspace %s (%s)\n", ws.ID, ws.Name)
	return nil
}

func runUnwatch(cmd *cobra.Command, args []string) error {
	workspaceID := args[0]

	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return err
	}

	if !cfg.RemoveWatchedWorkspace(workspaceID) {
		return fmt.Errorf("workspace %s is not being watched", workspaceID)
	}

	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Stopped watching workspace %s\n", workspaceID)
	return nil
}

type workspaceRepoEntry struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

// fetchWorkspaceRepos retrieves the current repos attached to a workspace.
// It returns the normalised slice plus the workspace ID for use in the PATCH.
func fetchWorkspaceRepos(ctx context.Context, client *cli.APIClient, wsID string) ([]workspaceRepoEntry, error) {
	var ws struct {
		Repos []workspaceRepoEntry `json:"repos"`
	}
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &ws); err != nil {
		return nil, fmt.Errorf("get workspace: %w", err)
	}
	return ws.Repos, nil
}

func saveWorkspaceRepos(ctx context.Context, client *cli.APIClient, wsID string, repos []workspaceRepoEntry) ([]workspaceRepoEntry, error) {
	// PATCH always replaces the full list; ensure non-nil slice so we can clear it.
	if repos == nil {
		repos = []workspaceRepoEntry{}
	}
	body := map[string]any{"repos": repos}
	var out struct {
		Repos []workspaceRepoEntry `json:"repos"`
	}
	if err := client.PatchJSON(ctx, "/api/workspaces/"+wsID, body, &out); err != nil {
		return nil, fmt.Errorf("update workspace repos: %w", err)
	}
	return out.Repos, nil
}

func runWorkspaceRepoList(cmd *cobra.Command, _ []string) error {
	wsID := resolveWorkspaceID(cmd)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: set MULTICA_WORKSPACE_ID or pass --workspace-id")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	repos, err := fetchWorkspaceRepos(ctx, client, wsID)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, repos)
	}

	if len(repos) == 0 {
		fmt.Fprintln(os.Stderr, "No repositories attached to this workspace.")
		return nil
	}

	headers := []string{"URL", "DESCRIPTION"}
	rows := make([][]string, 0, len(repos))
	for _, r := range repos {
		desc := r.Description
		if utf8.RuneCountInString(desc) > 80 {
			runes := []rune(desc)
			desc = string(runes[:77]) + "..."
		}
		rows = append(rows, []string{r.URL, desc})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runWorkspaceRepoAdd(cmd *cobra.Command, args []string) error {
	url := strings.TrimSpace(args[0])
	if url == "" {
		return fmt.Errorf("repo URL is required")
	}

	wsID := resolveWorkspaceID(cmd)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: set MULTICA_WORKSPACE_ID or pass --workspace-id")
	}

	description, _ := cmd.Flags().GetString("description")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	repos, err := fetchWorkspaceRepos(ctx, client, wsID)
	if err != nil {
		return err
	}

	for _, r := range repos {
		if r.URL == url {
			return fmt.Errorf("repo already attached to workspace: %s (use 'workspace repo update' to change its description)", url)
		}
	}

	repos = append(repos, workspaceRepoEntry{URL: url, Description: description})

	updated, err := saveWorkspaceRepos(ctx, client, wsID, repos)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Added repo %s (workspace now has %d repos).\n", url, len(updated))
	return nil
}

func runWorkspaceRepoRemove(cmd *cobra.Command, args []string) error {
	url := strings.TrimSpace(args[0])
	if url == "" {
		return fmt.Errorf("repo URL is required")
	}

	wsID := resolveWorkspaceID(cmd)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: set MULTICA_WORKSPACE_ID or pass --workspace-id")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	repos, err := fetchWorkspaceRepos(ctx, client, wsID)
	if err != nil {
		return err
	}

	filtered := make([]workspaceRepoEntry, 0, len(repos))
	removed := false
	for _, r := range repos {
		if r.URL == url {
			removed = true
			continue
		}
		filtered = append(filtered, r)
	}
	if !removed {
		return fmt.Errorf("repo not attached to workspace: %s", url)
	}

	updated, err := saveWorkspaceRepos(ctx, client, wsID, filtered)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Removed repo %s (workspace now has %d repos).\n", url, len(updated))
	return nil
}

func runWorkspaceRepoUpdate(cmd *cobra.Command, args []string) error {
	url := strings.TrimSpace(args[0])
	if url == "" {
		return fmt.Errorf("repo URL is required")
	}

	if !cmd.Flags().Changed("description") {
		return fmt.Errorf("--description is required")
	}
	description, _ := cmd.Flags().GetString("description")

	wsID := resolveWorkspaceID(cmd)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: set MULTICA_WORKSPACE_ID or pass --workspace-id")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	repos, err := fetchWorkspaceRepos(ctx, client, wsID)
	if err != nil {
		return err
	}

	found := false
	for i, r := range repos {
		if r.URL == url {
			repos[i].Description = description
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("repo not attached to workspace: %s (use 'workspace repo add' first)", url)
	}

	if _, err := saveWorkspaceRepos(ctx, client, wsID, repos); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Updated description for repo %s.\n", url)
	return nil
}
