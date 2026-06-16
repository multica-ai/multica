package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
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
	Use:   "get [workspace-id|slug|prefix]",
	Short: "Get workspace details",
	Long: "Prints the full details of a workspace. The argument accepts a full " +
		"UUID, a slug, or a short UUID prefix (≥4 hex chars) as shown in " +
		"'workspace list'. If omitted, the current default workspace is used.",
	Args: cobra.MaximumNArgs(1),
	RunE: runWorkspaceGet,
}

var workspaceMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Manage workspace members",
}

var workspaceMemberListCmd = &cobra.Command{
	Use:   "list [workspace-id|slug|prefix]",
	Short: "List workspace members",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceMembers,
}

var workspaceUpdateCmd = &cobra.Command{
	Use:   "update [workspace-id|slug|prefix]",
	Short: "Update workspace metadata (admin/owner only)",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runWorkspaceUpdate,
}

var workspaceSwitchCmd = &cobra.Command{
	Use:   "switch <workspace-id|slug|prefix>",
	Short: "Set the default workspace for this profile",
	Long: "Sets the default workspace for the current profile after verifying you " +
		"have access to it. Accepts a full UUID, a slug, or a short UUID " +
		"prefix (≥4 hex chars) as shown in 'workspace list'. Subsequent " +
		"commands without --workspace-id or MULTICA_WORKSPACE_ID will target " +
		"this workspace.\n\n" +
		"Resolution priority (highest to lowest): --workspace-id flag, " +
		"MULTICA_WORKSPACE_ID env, profile default (set by this command).\n\n" +
		"For low-level use, 'multica config set workspace_id <id>' writes the " +
		"same setting without verification.",
	Args: exactArgs(1),
	RunE: runWorkspaceSwitch,
}

func init() {
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceGetCmd)
	workspaceCmd.AddCommand(workspaceMemberCmd)
	workspaceMemberCmd.AddCommand(workspaceMemberListCmd)
	workspaceCmd.AddCommand(workspaceUpdateCmd)
	workspaceCmd.AddCommand(workspaceSwitchCmd)

	workspaceListCmd.Flags().String("output", "table", "Output format: table or json")
	workspaceListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	workspaceGetCmd.Flags().String("output", "json", "Output format: table or json")
	workspaceMemberListCmd.Flags().String("output", "table", "Output format: table or json")

	workspaceUpdateCmd.Flags().String("name", "", "New workspace name")
	workspaceUpdateCmd.Flags().String("description", "", "New description (decodes \\n, \\r, \\t, \\\\; pipe via --description-stdin to preserve literal backslashes)")
	workspaceUpdateCmd.Flags().Bool("description-stdin", false, "Read description from stdin (preserves multi-line content verbatim)")
	workspaceUpdateCmd.Flags().String("context", "", "New workspace context (decodes \\n, \\r, \\t, \\\\; pipe via --context-stdin to preserve literal backslashes)")
	workspaceUpdateCmd.Flags().Bool("context-stdin", false, "Read context from stdin (preserves multi-line content verbatim)")
	workspaceUpdateCmd.Flags().String("issue-prefix", "", "New issue prefix (uppercased server-side)")
	workspaceUpdateCmd.Flags().String("output", "json", "Output format: table or json")
}

// workspaceSummary is the subset of fields the CLI needs from /api/workspaces
// to drive list and switch. Keeping it here (instead of using the full
// WorkspaceResponse) avoids a dependency on the handler package.
type workspaceSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// fetchWorkspaces lists all workspaces the authenticated user belongs to. It
// is shared by `list` and `switch` so both see the same access-controlled view
// of workspaces.
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
	ctx, cancel := cli.APIContext(context.Background())
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
	fullID, _ := cmd.Flags().GetBool("full-id")
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "\tID\tNAME\tSLUG")
	for _, ws := range workspaces {
		marker := " "
		if ws.ID == currentID {
			marker = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", marker, displayID(ws.ID, fullID), ws.Name, ws.Slug)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if currentID != "" {
		fmt.Fprintln(os.Stderr, "\n* = current default workspace (use 'multica workspace switch <id|slug|prefix>' to change)")
	} else {
		fmt.Fprintln(os.Stderr, "\nNo default workspace set. Use 'multica workspace switch <id|slug|prefix>' to pick one.")
	}
	fmt.Fprintln(os.Stderr, "Tip: pass the ID column, SLUG, or full UUID (--full-id) to 'workspace get/update/switch'.")
	return nil
}

// resolveWorkspaceByIDOrSlug looks up a workspace in the caller's accessible
// list by full UUID, slug (case-insensitive), or short UUID prefix (≥4 hex
// chars). The matching order is exact UUID → exact slug → prefix, so a slug
// that happens to be a hex string can never be shadowed by a colliding UUID
// prefix. Returns an error if no workspace matches, which doubles as the
// "access denied / does not exist" check — the server only returns workspaces
// the user is a member of, so a match implies access.
func resolveWorkspaceByIDOrSlug(workspaces []workspaceSummary, target string) (workspaceSummary, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return workspaceSummary{}, fmt.Errorf("workspace id, slug, or id prefix is required")
	}
	// Slug comparison is case-insensitive (slugs are stored lowercase on the
	// server, but tolerate user-typed uppercase). UUIDs are also case-
	// insensitive in canonical form, so the lowering is safe for both.
	lowered := strings.ToLower(target)
	for _, ws := range workspaces {
		if strings.ToLower(ws.ID) == lowered {
			return ws, nil
		}
	}
	for _, ws := range workspaces {
		if ws.Slug != "" && strings.ToLower(ws.Slug) == lowered {
			return ws, nil
		}
	}

	// Fall back to short UUID prefix matching, so values copied from
	// `workspace list`'s default (truncated) ID column round-trip back into
	// get/update/switch. normalizeUUIDPrefix enforces ≥4 hex chars to avoid
	// surprises from arbitrary substrings.
	if prefix, err := normalizeUUIDPrefix(target); err == nil {
		matches := make([]workspaceSummary, 0, 1)
		for _, ws := range workspaces {
			if strings.HasPrefix(compactUUID(ws.ID), prefix) {
				matches = append(matches, ws)
			}
		}
		switch len(matches) {
		case 0:
			// fall through to the not-found error below
		case 1:
			return matches[0], nil
		default:
			return workspaceSummary{}, ambiguousWorkspacePrefixError(target, matches)
		}
	}

	return workspaceSummary{}, fmt.Errorf("workspace %q not found or you do not have access; run 'multica workspace list' to see options", target)
}

func ambiguousWorkspacePrefixError(input string, matches []workspaceSummary) error {
	parts := make([]string, 0, len(matches))
	for _, m := range matches {
		label := m.Name
		if m.Slug != "" {
			label = fmt.Sprintf("%s (%s)", m.Name, m.Slug)
		}
		parts = append(parts, fmt.Sprintf("  %s  %s", m.ID, label))
	}
	return fmt.Errorf("ambiguous workspace id prefix %q; matches:\n%s\nUse more characters, the slug, or the full UUID", input, strings.Join(parts, "\n"))
}

// resolveWorkspaceRef fetches the caller's workspaces and resolves the input
// (UUID, slug, or short UUID prefix) to a workspaceSummary. Shared by
// `workspace get`, `workspace update`, `workspace member list`, and
// `workspace switch` so all four accept the same identifiers users see in
// `workspace list`.
func resolveWorkspaceRef(ctx context.Context, cmd *cobra.Command, input string) (workspaceSummary, error) {
	target := strings.TrimSpace(input)
	if target == "" {
		return workspaceSummary{}, fmt.Errorf("workspace id, slug, or id prefix is required")
	}
	workspaces, err := fetchWorkspaces(ctx, cmd)
	if err != nil {
		return workspaceSummary{}, err
	}
	return resolveWorkspaceByIDOrSlug(workspaces, target)
}

func runWorkspaceSwitch(cmd *cobra.Command, args []string) error {
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	ws, err := resolveWorkspaceRef(ctx, cmd, args[0])
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

// resolveWorkspaceArg returns the canonical UUID for a workspace command that
// takes an optional `[workspace-id]` arg. When the arg is supplied it is
// resolved against the caller's workspace list (UUID, slug, or short prefix);
// when omitted it falls back to the standard --workspace-id / env / profile
// resolution chain — the caller is responsible for guarding against the empty
// case. A full UUID is forwarded as-is to avoid an extra /api/workspaces
// round trip; access control is enforced by the downstream endpoint.
func resolveWorkspaceArg(cmd *cobra.Command, args []string) (string, error) {
	if len(args) > 0 {
		trimmed := strings.TrimSpace(args[0])
		if uuidRegexp.MatchString(trimmed) {
			return trimmed, nil
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()
		ws, err := resolveWorkspaceRef(ctx, cmd, trimmed)
		if err != nil {
			return "", err
		}
		return ws.ID, nil
	}
	return resolveWorkspaceID(cmd), nil
}

func runWorkspaceGet(cmd *cobra.Command, args []string) error {
	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
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
	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set MULTICA_WORKSPACE_ID")
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

	ctx, cancel := cli.APIContext(context.Background())
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
	wsID, err := resolveWorkspaceArg(cmd, args)
	if err != nil {
		return err
	}
	if wsID == "" {
		return fmt.Errorf("workspace ID is required: pass an id/slug/prefix as argument or set MULTICA_WORKSPACE_ID")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
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
