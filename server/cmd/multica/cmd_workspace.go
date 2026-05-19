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

var workspaceUpdateCmd = &cobra.Command{
	Use:   "update [workspace-id]",
	Short: "Update workspace metadata (admin/owner only)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceUpdate,
}

var workspaceSwitchCmd = &cobra.Command{
	Use:   "switch <workspace-id|slug>",
	Short: "Set the default workspace for this profile",
	Long: "Sets the default workspace for the current profile after verifying you " +
		"have access to it. Subsequent commands without --workspace-id or " +
		"MULTICA_WORKSPACE_ID will target this workspace.\n\n" +
		"Resolution priority (highest to lowest): --workspace-id flag, " +
		"MULTICA_WORKSPACE_ID env, profile default (set by this command).\n\n" +
		"For low-level use, 'multica config set workspace_id <id>' writes the " +
		"same setting without verification.",
	Args: exactArgs(1),
	RunE: runWorkspaceSwitch,
}

var workspaceCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show the current default workspace",
	RunE:  runWorkspaceCurrent,
}

func init() {
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceGetCmd)
	workspaceCmd.AddCommand(workspaceMembersCmd)
	workspaceCmd.AddCommand(workspaceUpdateCmd)
	workspaceCmd.AddCommand(workspaceSwitchCmd)
	workspaceCmd.AddCommand(workspaceCurrentCmd)

	workspaceListCmd.Flags().String("output", "table", "Output format: table or json")
	workspaceGetCmd.Flags().String("output", "json", "Output format: table or json")
	workspaceMembersCmd.Flags().String("output", "table", "Output format: table or json")
	workspaceCurrentCmd.Flags().String("output", "table", "Output format: table or json")

	workspaceUpdateCmd.Flags().String("name", "", "New workspace name")
	workspaceUpdateCmd.Flags().String("description", "", "New description (decodes \\n, \\r, \\t, \\\\; pipe via --description-stdin to preserve literal backslashes)")
	workspaceUpdateCmd.Flags().Bool("description-stdin", false, "Read description from stdin (preserves multi-line content verbatim)")
	workspaceUpdateCmd.Flags().String("context", "", "New workspace context (decodes \\n, \\r, \\t, \\\\; pipe via --context-stdin to preserve literal backslashes)")
	workspaceUpdateCmd.Flags().Bool("context-stdin", false, "Read context from stdin (preserves multi-line content verbatim)")
	workspaceUpdateCmd.Flags().String("issue-prefix", "", "New issue prefix (uppercased server-side)")
	workspaceUpdateCmd.Flags().String("output", "json", "Output format: table or json")
}

// workspaceSummary is the subset of fields the CLI needs from /api/workspaces
// to drive list/switch/current. Keeping it here (instead of using the full
// WorkspaceResponse) avoids a dependency on the handler package.
type workspaceSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// fetchWorkspaces lists all workspaces the authenticated user belongs to. It
// is shared by `list`, `switch`, and `current` so all three see the same
// access-controlled view of workspaces.
func fetchWorkspaces(ctx context.Context, cmd *cobra.Command) ([]workspaceSummary, error) {
	serverURL := resolveServerURL(cmd)
	token := resolveToken(cmd)
	if token == "" {
		return nil, fmt.Errorf("not authenticated: run 'multica login' first")
	}

	client := cli.NewAPIClient(serverURL, "", token)
	var workspaces []workspaceSummary
	if err := client.GetJSON(ctx, "/api/workspaces", &workspaces); err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return workspaces, nil
}

func runWorkspaceList(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	workspaces, err := fetchWorkspaces(ctx, cmd)
	if err != nil {
		return err
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, workspaces)
	}

	if len(workspaces) == 0 {
		fmt.Fprintln(os.Stderr, "No workspaces found.")
		return nil
	}

	currentID := resolveWorkspaceID(cmd)
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "\tID\tNAME\tSLUG")
	for _, ws := range workspaces {
		marker := " "
		if ws.ID == currentID {
			marker = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", marker, ws.ID, ws.Name, ws.Slug)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if currentID != "" {
		fmt.Fprintln(os.Stderr, "\n* = current default workspace (use 'multica workspace switch <id|slug>' to change)")
	} else {
		fmt.Fprintln(os.Stderr, "\nNo default workspace set. Use 'multica workspace switch <id|slug>' to pick one.")
	}
	return nil
}

// resolveWorkspaceByIDOrSlug looks up a workspace in the caller's accessible
// list by either UUID or slug. It returns an error if no workspace matches,
// which doubles as the "access denied / does not exist" check — the server
// only returns workspaces the user is a member of, so a match implies access.
func resolveWorkspaceByIDOrSlug(workspaces []workspaceSummary, target string) (workspaceSummary, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return workspaceSummary{}, fmt.Errorf("workspace id or slug is required")
	}
	// Slug comparison is case-insensitive (slugs are stored lowercase on the
	// server, but tolerate user-typed uppercase). UUIDs are also case-
	// insensitive in canonical form, so the lowering is safe for both.
	lowered := strings.ToLower(target)
	for _, ws := range workspaces {
		if ws.ID == target || strings.ToLower(ws.ID) == lowered {
			return ws, nil
		}
		if ws.Slug != "" && strings.ToLower(ws.Slug) == lowered {
			return ws, nil
		}
	}
	return workspaceSummary{}, fmt.Errorf("workspace %q not found or you do not have access; run 'multica workspace list' to see options", target)
}

func runWorkspaceSwitch(cmd *cobra.Command, args []string) error {
	target := args[0]

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	workspaces, err := fetchWorkspaces(ctx, cmd)
	if err != nil {
		return err
	}

	ws, err := resolveWorkspaceByIDOrSlug(workspaces, target)
	if err != nil {
		return err
	}

	profile := resolveProfile(cmd)
	cfg, err := cli.LoadCLIConfigForProfile(profile)
	if err != nil {
		return err
	}
	cfg.WorkspaceID = ws.ID
	if err := cli.SaveCLIConfigForProfile(cfg, profile); err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "Switched to workspace: %s (%s)\n", ws.Name, ws.ID)
	return nil
}

func runWorkspaceCurrent(cmd *cobra.Command, _ []string) error {
	currentID := resolveWorkspaceID(cmd)
	if currentID == "" {
		return fmt.Errorf("no default workspace set: use 'multica workspace switch <id|slug>' to pick one")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var ws map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+currentID, &ws); err != nil {
		return fmt.Errorf("get workspace: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, ws)
	}

	headers := []string{"ID", "NAME", "SLUG", "ISSUE PREFIX"}
	rows := [][]string{{
		strVal(ws, "id"),
		strVal(ws, "name"),
		strVal(ws, "slug"),
		strVal(ws, "issue_prefix"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
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

// buildWorkspaceUpdateBody assembles the PATCH payload from the flags the
// caller actually set, mirroring server/internal/handler/workspace.go's
// UpdateWorkspaceRequest. Only fields whose flag is Changed() are emitted, so
// the caller cannot accidentally clobber a field they did not pass.
func buildWorkspaceUpdateBody(cmd *cobra.Command) (map[string]any, error) {
	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		body["name"] = v
	}
	if cmd.Flags().Changed("description") || cmd.Flags().Changed("description-stdin") {
		desc, _, err := resolveTextFlag(cmd, "description")
		if err != nil {
			return nil, err
		}
		body["description"] = desc
	}
	if cmd.Flags().Changed("context") || cmd.Flags().Changed("context-stdin") {
		ctxText, _, err := resolveTextFlag(cmd, "context")
		if err != nil {
			return nil, err
		}
		body["context"] = ctxText
	}
	if cmd.Flags().Changed("issue-prefix") {
		v, _ := cmd.Flags().GetString("issue-prefix")
		// The handler silently skips an empty prefix (workspace.go:274), so
		// `--issue-prefix ""` would otherwise return 200 without changing
		// anything. Reject it here so the failure is visible.
		if strings.TrimSpace(v) == "" {
			return nil, fmt.Errorf("--issue-prefix cannot be empty; clearing the prefix is not supported")
		}
		body["issue_prefix"] = v
	}
	return body, nil
}

func runWorkspaceUpdate(cmd *cobra.Command, args []string) error {
	wsID := workspaceIDFromArgs(cmd, args)
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass as argument or set MULTICA_WORKSPACE_ID")
	}

	body, err := buildWorkspaceUpdateBody(cmd)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --name, --description, --context, or --issue-prefix")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var ws map[string]any
	if err := client.PatchJSON(ctx, "/api/workspaces/"+wsID, body, &ws); err != nil {
		return fmt.Errorf("update workspace: %w", err)
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
