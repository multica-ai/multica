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

var workspaceRepoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage workspace repositories",
}

var workspaceRepoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List repositories in the workspace",
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
	Short: "Update a repository's description",
	Args:  exactArgs(1),
	RunE:  runWorkspaceRepoUpdate,
}

func init() {
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceGetCmd)
	workspaceCmd.AddCommand(workspaceMembersCmd)
	workspaceCmd.AddCommand(workspaceRepoCmd)

	workspaceRepoCmd.AddCommand(workspaceRepoListCmd)
	workspaceRepoCmd.AddCommand(workspaceRepoAddCmd)
	workspaceRepoCmd.AddCommand(workspaceRepoRemoveCmd)
	workspaceRepoCmd.AddCommand(workspaceRepoUpdateCmd)

	workspaceGetCmd.Flags().String("output", "json", "Output format: table or json")
	workspaceMembersCmd.Flags().String("output", "table", "Output format: table or json")

	workspaceRepoListCmd.Flags().String("output", "table", "Output format: table or json")
	workspaceRepoAddCmd.Flags().String("description", "", "Repository description")
	workspaceRepoAddCmd.Flags().String("output", "json", "Output format: table or json")
	workspaceRepoUpdateCmd.Flags().String("description", "", "New repository description")
	workspaceRepoUpdateCmd.Flags().String("output", "json", "Output format: table or json")
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

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME")
	for _, ws := range workspaces {
		fmt.Fprintf(w, "%s\t%s\n", ws.ID, ws.Name)
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

// fetchWorkspaceRepos returns the workspace's current repos slice and the
// resolved workspace ID. Repos are normalized to []map[string]any so callers
// can mutate and PATCH them back.
func fetchWorkspaceRepos(ctx context.Context, cmd *cobra.Command) (string, []map[string]any, error) {
	wsID, err := requireWorkspaceID(cmd)
	if err != nil {
		return "", nil, err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return "", nil, err
	}

	var ws map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+wsID, &ws); err != nil {
		return "", nil, fmt.Errorf("get workspace: %w", err)
	}

	repos := normalizeRepos(ws["repos"])
	return wsID, repos, nil
}

func normalizeRepos(raw any) []map[string]any {
	list, ok := raw.([]any)
	if !ok {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func patchWorkspaceRepos(ctx context.Context, cmd *cobra.Command, wsID string, repos []map[string]any) (map[string]any, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	body := map[string]any{"repos": repos}
	var result map[string]any
	if err := client.PatchJSON(ctx, "/api/workspaces/"+wsID, body, &result); err != nil {
		return nil, fmt.Errorf("update workspace: %w", err)
	}
	return result, nil
}

func printRepos(cmd *cobra.Command, repos []map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, repos)
	}

	if len(repos) == 0 {
		fmt.Fprintln(os.Stderr, "No repositories configured.")
		return nil
	}

	headers := []string{"URL", "DESCRIPTION"}
	rows := make([][]string, 0, len(repos))
	for _, r := range repos {
		rows = append(rows, []string{
			strVal(r, "url"),
			strVal(r, "description"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runWorkspaceRepoList(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	_, repos, err := fetchWorkspaceRepos(ctx, cmd)
	if err != nil {
		return err
	}
	return printRepos(cmd, repos)
}

func runWorkspaceRepoAdd(cmd *cobra.Command, args []string) error {
	url := strings.TrimSpace(args[0])
	if url == "" {
		return fmt.Errorf("repository URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	wsID, repos, err := fetchWorkspaceRepos(ctx, cmd)
	if err != nil {
		return err
	}

	for _, r := range repos {
		if strVal(r, "url") == url {
			return fmt.Errorf("repository %q is already in the workspace", url)
		}
	}

	description, _ := cmd.Flags().GetString("description")
	repos = append(repos, map[string]any{
		"url":         url,
		"description": description,
	})

	result, err := patchWorkspaceRepos(ctx, cmd, wsID, repos)
	if err != nil {
		return err
	}

	updated := normalizeRepos(result["repos"])
	fmt.Fprintf(os.Stderr, "Added %s to workspace.\n", url)
	return printRepos(cmd, updated)
}

func runWorkspaceRepoRemove(cmd *cobra.Command, args []string) error {
	url := strings.TrimSpace(args[0])
	if url == "" {
		return fmt.Errorf("repository URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	wsID, repos, err := fetchWorkspaceRepos(ctx, cmd)
	if err != nil {
		return err
	}

	filtered := make([]map[string]any, 0, len(repos))
	found := false
	for _, r := range repos {
		if strVal(r, "url") == url {
			found = true
			continue
		}
		filtered = append(filtered, r)
	}
	if !found {
		return fmt.Errorf("repository %q not found in workspace", url)
	}

	result, err := patchWorkspaceRepos(ctx, cmd, wsID, filtered)
	if err != nil {
		return err
	}

	updated := normalizeRepos(result["repos"])
	fmt.Fprintf(os.Stderr, "Removed %s from workspace.\n", url)
	return printRepos(cmd, updated)
}

func runWorkspaceRepoUpdate(cmd *cobra.Command, args []string) error {
	url := strings.TrimSpace(args[0])
	if url == "" {
		return fmt.Errorf("repository URL is required")
	}

	if !cmd.Flags().Changed("description") {
		return fmt.Errorf("no fields to update; use --description")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	wsID, repos, err := fetchWorkspaceRepos(ctx, cmd)
	if err != nil {
		return err
	}

	description, _ := cmd.Flags().GetString("description")
	found := false
	for _, r := range repos {
		if strVal(r, "url") == url {
			r["description"] = description
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("repository %q not found in workspace", url)
	}

	result, err := patchWorkspaceRepos(ctx, cmd, wsID, repos)
	if err != nil {
		return err
	}

	updated := normalizeRepos(result["repos"])
	fmt.Fprintf(os.Stderr, "Updated %s.\n", url)
	return printRepos(cmd, updated)
}
