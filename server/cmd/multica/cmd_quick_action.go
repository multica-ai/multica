package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// multica quick-action {list|get|create|update|archive|restore|delete|run} —
// workspace quick action presets ("who to call and what to say" on an issue).
// See server/internal/handler/quick_action.go for the server contract:
//   - public actions require workspace owner/admin and a publicly-invocable target;
//   - private actions belong to their creator and may bind any target the creator can invoke;
//   - targetless actions omit both assignee_type and assignee_id and post the prompt verbatim;
//   - management writes (create/update/archive/restore/delete) require a human member.

type quickActionDTO struct {
	ID            string  `json:"id"`
	WorkspaceID   string  `json:"workspace_id"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	AssigneeType  string  `json:"assignee_type"`
	AssigneeID    string  `json:"assignee_id"`
	Prompt        string  `json:"prompt"`
	Visibility    string  `json:"visibility"`
	Status        string  `json:"status"`
	LastUsedAt    *string `json:"last_used_at"`
	UseCount      int64   `json:"use_count"`
	CreatedByID   string  `json:"created_by_id"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	TargetName    string  `json:"target_name,omitempty"`
	TargetPublic  bool    `json:"target_public"`
	TargetMissing bool    `json:"target_missing"`
}

var quickActionCmd = &cobra.Command{
	Use:   "quick-action",
	Short: "Manage and run workspace quick actions",
	Long: `Manage and run workspace quick actions — saved prompts (with an optional
agent or squad target) that post to an issue in one step.

Actions can be addressed by full UUID, UUID prefix, or exact name
(case-insensitive). Names prefer active actions when an archived action
shares the same name.

Creating, updating, archiving, restoring, and deleting quick actions require
a human member (public actions require workspace owner/admin; private actions
are personal to their creator).`,
}

var quickActionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List quick actions in the workspace",
	Args:  exactArgs(0),
	RunE:  runQuickActionList,
}

var quickActionGetCmd = &cobra.Command{
	Use:   "get <id-or-name>",
	Short: "Show one quick action",
	Args:  exactArgs(1),
	RunE:  runQuickActionGet,
}

var quickActionCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a quick action",
	Long: `Create a quick action preset in the current workspace.

  multica quick-action create --name "Merge" --prompt "merge"
  multica quick-action create --name "Review" --agent Lambda --prompt-file ./review.md
  multica quick-action create --name "Handoff" --squad JSquad --visibility private --prompt "Take over from here."

Omit --agent and --squad to create a targetless action that posts the prompt
without @mentioning anyone. Visibility defaults to private; pass --visibility public
to share with the workspace (requires owner/admin and a publicly-invocable target).`,
	Args: exactArgs(0),
	RunE: runQuickActionCreate,
}

var quickActionUpdateCmd = &cobra.Command{
	Use:   "update <id-or-name>",
	Short: "Update a quick action",
	Long: `Update one or more fields of a quick action. Omitted flags keep their
current values.

  multica quick-action update "Review" --squad JSquad
  multica quick-action update "Review" --clear-target
  multica quick-action update "Review" --prompt-file ./review.md`,
	Args: exactArgs(1),
	RunE: runQuickActionUpdate,
}

var quickActionArchiveCmd = &cobra.Command{
	Use:   "archive <id-or-name>",
	Short: "Archive a quick action",
	Args:  exactArgs(1),
	RunE:  makeQuickActionStatusRun("archived"),
}

var quickActionRestoreCmd = &cobra.Command{
	Use:     "restore <id-or-name>",
	Aliases: []string{"unarchive"},
	Short:   "Restore an archived quick action",
	Args:    exactArgs(1),
	RunE:    makeQuickActionStatusRun("active"),
}

var quickActionDeleteCmd = &cobra.Command{
	Use:   "delete <id-or-name>",
	Short: "Permanently delete a quick action",
	Args:  exactArgs(1),
	RunE:  runQuickActionDelete,
}

var quickActionRunCmd = &cobra.Command{
	Use:   "run <id-or-name> <issue-id>",
	Short: "Run a quick action on an issue",
	Long: `Run a saved quick action on an issue, posting its comment (and optional
agent or squad mention) through the normal comment trigger path.

Pass --dry-run to preview the rendered comment body without posting it.`,
	Args: exactArgs(2),
	RunE: runQuickActionRun,
}

func init() {
	quickActionCmd.AddCommand(quickActionListCmd)
	quickActionCmd.AddCommand(quickActionGetCmd)
	quickActionCmd.AddCommand(quickActionCreateCmd)
	quickActionCmd.AddCommand(quickActionUpdateCmd)
	quickActionCmd.AddCommand(quickActionArchiveCmd)
	quickActionCmd.AddCommand(quickActionRestoreCmd)
	quickActionCmd.AddCommand(quickActionDeleteCmd)
	quickActionCmd.AddCommand(quickActionRunCmd)

	quickActionListCmd.Flags().String("output", "table", "Output format: table or json")
	quickActionListCmd.Flags().Bool("include-archived", false, "Include archived quick actions")
	quickActionListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	quickActionGetCmd.Flags().String("output", "json", "Output format: table or json")

	quickActionCreateCmd.Flags().String("output", "table", "Output format: table or json")
	quickActionCreateCmd.Flags().String("name", "", "Quick action name (required, max 32 chars)")
	quickActionCreateCmd.Flags().String("description", "", "Short description (max 200 chars)")
	quickActionCreateCmd.Flags().String("prompt", "", "Prompt text sent verbatim when run (max 4000 chars)")
	quickActionCreateCmd.Flags().String("prompt-file", "", "Read prompt from a UTF-8 file inside the working directory")
	quickActionCreateCmd.Flags().Bool("prompt-stdin", false, "Read prompt from stdin")
	quickActionCreateCmd.Flags().Bool("allow-external-file", false, "Allow --prompt-file outside the current working directory")
	quickActionCreateCmd.Flags().String("agent", "", "Target agent name or UUID (mutually exclusive with --squad)")
	quickActionCreateCmd.Flags().String("squad", "", "Target squad name or UUID (mutually exclusive with --agent)")
	quickActionCreateCmd.Flags().String("visibility", "private", "Visibility: private or public")

	quickActionUpdateCmd.Flags().String("output", "table", "Output format: table or json")
	quickActionUpdateCmd.Flags().String("name", "", "New name")
	quickActionUpdateCmd.Flags().String("description", "", "New description (pass empty string to clear)")
	quickActionUpdateCmd.Flags().String("prompt", "", "New prompt text")
	quickActionUpdateCmd.Flags().String("prompt-file", "", "Read new prompt from a UTF-8 file inside the working directory")
	quickActionUpdateCmd.Flags().Bool("prompt-stdin", false, "Read new prompt from stdin")
	quickActionUpdateCmd.Flags().Bool("allow-external-file", false, "Allow --prompt-file outside the current working directory")
	quickActionUpdateCmd.Flags().String("agent", "", "Set target to an agent name or UUID")
	quickActionUpdateCmd.Flags().String("squad", "", "Set target to a squad name or UUID")
	quickActionUpdateCmd.Flags().Bool("clear-target", false, "Remove the configured target (make the action targetless)")
	quickActionUpdateCmd.Flags().String("visibility", "", "New visibility: private or public")
	quickActionUpdateCmd.Flags().String("status", "", "New status: active or archived")

	quickActionArchiveCmd.Flags().String("output", "table", "Output format: table or json")
	quickActionRestoreCmd.Flags().String("output", "table", "Output format: table or json")
	quickActionDeleteCmd.Flags().String("output", "table", "Output format: table or json")

	quickActionRunCmd.Flags().String("output", "table", "Output format: table or json")
	quickActionRunCmd.Flags().Bool("dry-run", false, "Preview the rendered comment body without posting it")
}

func fetchQuickActions(ctx context.Context, client *cli.APIClient, includeArchived bool) ([]quickActionDTO, error) {
	path := "/api/quick-actions"
	if includeArchived {
		path += "?include_archived=true"
	}
	var resp struct {
		QuickActions []quickActionDTO `json:"quick_actions"`
	}
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("list quick actions: %w", err)
	}
	if resp.QuickActions == nil {
		return []quickActionDTO{}, nil
	}
	return resp.QuickActions, nil
}

// resolveQuickActionRef matches a quick action by full UUID, exact
// case-insensitive name (preferring active over archived), or UUID prefix.
func resolveQuickActionRef(actions []quickActionDTO, ref string) (quickActionDTO, error) {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return quickActionDTO{}, fmt.Errorf("quick action ID or name is required")
	}

	for _, a := range actions {
		if a.ID == trimmed {
			return a, nil
		}
	}

	var active, archived []quickActionDTO
	for _, a := range actions {
		if !strings.EqualFold(a.Name, trimmed) {
			continue
		}
		if a.Status == "archived" {
			archived = append(archived, a)
		} else {
			active = append(active, a)
		}
	}
	matches := active
	if len(matches) == 0 {
		matches = archived
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		cands := make([]idCandidate, 0, len(matches))
		for _, a := range matches {
			cands = append(cands, idCandidate{ID: a.ID})
		}
		return quickActionDTO{}, fmt.Errorf("more than one quick action is named %q; pass its ID:\n%s", trimmed, joinCandidateIDs(cands))
	}

	if prefix, err := normalizeUUIDPrefix(trimmed); err == nil {
		var prefixMatches []idCandidate
		var found quickActionDTO
		for _, a := range actions {
			if strings.HasPrefix(compactUUID(a.ID), prefix) {
				prefixMatches = append(prefixMatches, idCandidate{ID: a.ID})
				found = a
			}
		}
		switch len(prefixMatches) {
		case 1:
			return found, nil
		case 0:
			// Fall through to unified not-found error.
		default:
			return quickActionDTO{}, ambiguousIDPrefixError("quick action", trimmed, prefixMatches)
		}
	}

	return quickActionDTO{}, fmt.Errorf("quick action %q not found; run `multica quick-action list --include-archived`", trimmed)
}

func formatQuickActionTarget(a quickActionDTO, fullID bool) string {
	if a.AssigneeType == "" {
		return "-"
	}
	if a.TargetMissing {
		return fmt.Sprintf("%s:%s (missing)", a.AssigneeType, displayID(a.AssigneeID, fullID))
	}
	name := a.TargetName
	if name == "" {
		name = displayID(a.AssigneeID, fullID)
	}
	return fmt.Sprintf("%s (%s)", name, a.AssigneeType)
}

func printQuickActionTable(actions []quickActionDTO, fullID bool) {
	headers := []string{"ID", "NAME", "TARGET", "VISIBILITY", "STATUS", "USES"}
	rows := make([][]string, 0, len(actions))
	for _, a := range actions {
		rows = append(rows, []string{
			displayID(a.ID, fullID),
			a.Name,
			formatQuickActionTarget(a, fullID),
			a.Visibility,
			a.Status,
			strconv.FormatInt(a.UseCount, 10),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func printQuickActionDetail(a quickActionDTO) {
	fmt.Fprintf(os.Stdout, "ID:          %s\n", a.ID)
	fmt.Fprintf(os.Stdout, "Name:        %s\n", a.Name)
	if a.Description != "" {
		fmt.Fprintf(os.Stdout, "Description: %s\n", a.Description)
	}
	fmt.Fprintf(os.Stdout, "Target:      %s\n", formatQuickActionTarget(a, true))
	fmt.Fprintf(os.Stdout, "Visibility:  %s\n", a.Visibility)
	fmt.Fprintf(os.Stdout, "Status:      %s\n", a.Status)
	fmt.Fprintf(os.Stdout, "Uses:        %d\n", a.UseCount)
	if a.LastUsedAt != nil && *a.LastUsedAt != "" {
		fmt.Fprintf(os.Stdout, "Last Used:   %s\n", *a.LastUsedAt)
	}
	fmt.Fprintf(os.Stdout, "Prompt:\n%s\n", a.Prompt)
}

func validateQuickActionVisibilityFlag(v string) (string, error) {
	normalized := strings.TrimSpace(v)
	if normalized != "private" && normalized != "public" {
		return "", fmt.Errorf("invalid --visibility %q; valid values: private, public", v)
	}
	return normalized, nil
}

func resolveQuickActionTargetFlags(
	ctx context.Context,
	client *cli.APIClient,
	cmd *cobra.Command,
	allowClear bool,
) (assigneeType, assigneeID string, changed bool, err error) {
	agentSet := cmd.Flags().Changed("agent")
	squadSet := cmd.Flags().Changed("squad")
	clearSet := allowClear && cmd.Flags().Changed("clear-target")
	clearVal := false
	if clearSet {
		clearVal, _ = cmd.Flags().GetBool("clear-target")
	}

	activeTargets := 0
	if agentSet {
		activeTargets++
	}
	if squadSet {
		activeTargets++
	}
	if clearVal {
		activeTargets++
	}
	if activeTargets > 1 {
		if allowClear {
			return "", "", false, fmt.Errorf("--agent, --squad, and --clear-target are mutually exclusive")
		}
		return "", "", false, fmt.Errorf("--agent and --squad are mutually exclusive")
	}
	if activeTargets == 0 {
		return "", "", false, nil
	}

	if clearVal {
		return "", "", true, nil
	}
	if agentSet {
		ref, _ := cmd.Flags().GetString("agent")
		if strings.TrimSpace(ref) == "" {
			if allowClear {
				return "", "", false, fmt.Errorf("--agent cannot be empty; use --clear-target to remove the target")
			}
			return "", "", false, fmt.Errorf("--agent cannot be empty; omit --agent for a targetless quick action")
		}
		id, err := resolveAgent(ctx, client, ref)
		if err != nil {
			return "", "", false, fmt.Errorf("resolve agent: %w", err)
		}
		return "agent", id, true, nil
	}

	ref, _ := cmd.Flags().GetString("squad")
	if strings.TrimSpace(ref) == "" {
		if allowClear {
			return "", "", false, fmt.Errorf("--squad cannot be empty; use --clear-target to remove the target")
		}
		return "", "", false, fmt.Errorf("--squad cannot be empty; omit --squad for a targetless quick action")
	}
	id, err := resolveSquad(ctx, client, ref)
	if err != nil {
		return "", "", false, fmt.Errorf("resolve squad: %w", err)
	}
	return "squad", id, true, nil
}

// ── List ────────────────────────────────────────────────────────────────────

func runQuickActionList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	includeArchived, _ := cmd.Flags().GetBool("include-archived")
	actions, err := fetchQuickActions(ctx, client, includeArchived)
	if err != nil {
		return err
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, actions)
	}
	if len(actions) == 0 {
		fmt.Fprintln(os.Stderr, "No quick actions found.")
		return nil
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	printQuickActionTable(actions, fullID)
	return nil
}

// ── Get ─────────────────────────────────────────────────────────────────────

func runQuickActionGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	actions, err := fetchQuickActions(ctx, client, true)
	if err != nil {
		return err
	}
	action, err := resolveQuickActionRef(actions, args[0])
	if err != nil {
		return err
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, action)
	}
	printQuickActionDetail(action)
	return nil
}

// ── Create ──────────────────────────────────────────────────────────────────

func runQuickActionCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("--name is required")
	}

	prompt, hasPrompt, err := resolveTextFlag(cmd, "prompt")
	if err != nil {
		return err
	}
	if !hasPrompt || strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("--prompt, --prompt-file, or --prompt-stdin is required")
	}

	visibilityRaw, _ := cmd.Flags().GetString("visibility")
	visibility, err := validateQuickActionVisibilityFlag(visibilityRaw)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	assigneeType, assigneeID, hasTarget, err := resolveQuickActionTargetFlags(ctx, client, cmd, false)
	if err != nil {
		return err
	}

	body := map[string]any{
		"name":       name,
		"prompt":     prompt,
		"visibility": visibility,
	}
	if desc, _ := cmd.Flags().GetString("description"); desc != "" {
		body["description"] = desc
	}
	if hasTarget {
		body["assignee_type"] = assigneeType
		body["assignee_id"] = assigneeID
	}

	var created quickActionDTO
	if err := client.PostJSON(ctx, "/api/quick-actions", body, &created); err != nil {
		return fmt.Errorf("create quick action: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, created)
	}
	fmt.Fprintf(os.Stdout, "Quick action %q created (%s).\n", created.Name, created.ID)
	printQuickActionTable([]quickActionDTO{created}, false)
	return nil
}

// ── Update ──────────────────────────────────────────────────────────────────

func runQuickActionUpdate(cmd *cobra.Command, args []string) error {
	prompt, hasPrompt, err := resolveTextFlag(cmd, "prompt")
	if err != nil {
		return err
	}
	if cmd.Flags().Changed("prompt") && strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("--prompt cannot be empty")
	}

	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("--name cannot be empty")
		}
		body["name"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	if hasPrompt {
		body["prompt"] = prompt
	}
	if cmd.Flags().Changed("visibility") {
		v, _ := cmd.Flags().GetString("visibility")
		normalized, err := validateQuickActionVisibilityFlag(v)
		if err != nil {
			return err
		}
		body["visibility"] = normalized
	}
	if cmd.Flags().Changed("status") {
		v, _ := cmd.Flags().GetString("status")
		v = strings.TrimSpace(v)
		if v != "active" && v != "archived" {
			return fmt.Errorf("invalid --status %q; valid values: active, archived", v)
		}
		body["status"] = v
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	assigneeType, assigneeID, targetChanged, err := resolveQuickActionTargetFlags(ctx, client, cmd, true)
	if err != nil {
		return err
	}
	if targetChanged {
		body["assignee_type"] = assigneeType
		body["assignee_id"] = assigneeID
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --name, --description, --prompt, --agent, --squad, --clear-target, --visibility, or --status")
	}

	actions, err := fetchQuickActions(ctx, client, true)
	if err != nil {
		return err
	}
	action, err := resolveQuickActionRef(actions, args[0])
	if err != nil {
		return err
	}

	var updated quickActionDTO
	if err := client.PatchJSON(ctx, "/api/quick-actions/"+url.PathEscape(action.ID), body, &updated); err != nil {
		return fmt.Errorf("update quick action: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, updated)
	}
	fmt.Fprintf(os.Stdout, "Quick action %q updated (%s).\n", updated.Name, updated.ID)
	printQuickActionTable([]quickActionDTO{updated}, false)
	return nil
}

// ── Archive / Restore ───────────────────────────────────────────────────────

func makeQuickActionStatusRun(status string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()

		actions, err := fetchQuickActions(ctx, client, true)
		if err != nil {
			return err
		}
		action, err := resolveQuickActionRef(actions, args[0])
		if err != nil {
			return err
		}

		var updated quickActionDTO
		body := map[string]any{"status": status}
		if err := client.PatchJSON(ctx, "/api/quick-actions/"+url.PathEscape(action.ID), body, &updated); err != nil {
			if status == "archived" {
				return fmt.Errorf("archive quick action: %w", err)
			}
			return fmt.Errorf("restore quick action: %w", err)
		}

		if output, _ := cmd.Flags().GetString("output"); output == "json" {
			return cli.PrintJSON(os.Stdout, updated)
		}
		verb := "restored"
		if status == "archived" {
			verb = "archived"
		}
		fmt.Fprintf(os.Stdout, "Quick action %q %s (%s).\n", updated.Name, verb, updated.ID)
		return nil
	}
}

// ── Delete ──────────────────────────────────────────────────────────────────

func runQuickActionDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	actions, err := fetchQuickActions(ctx, client, true)
	if err != nil {
		return err
	}
	action, err := resolveQuickActionRef(actions, args[0])
	if err != nil {
		return err
	}

	if err := client.DeleteJSON(ctx, "/api/quick-actions/"+url.PathEscape(action.ID)); err != nil {
		return fmt.Errorf("delete quick action: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"id": action.ID, "name": action.Name, "deleted": true})
	}
	fmt.Fprintf(os.Stdout, "Quick action %q deleted (%s).\n", action.Name, action.ID)
	return nil
}

// ── Run ─────────────────────────────────────────────────────────────────────

func runQuickActionRun(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	actions, err := fetchQuickActions(ctx, client, true)
	if err != nil {
		return err
	}
	action, err := resolveQuickActionRef(actions, args[0])
	if err != nil {
		return err
	}

	issue, err := resolveIssueRef(ctx, client, args[1])
	if err != nil {
		return err
	}

	basePath := fmt.Sprintf("/api/issues/%s/quick-actions/%s", url.PathEscape(issue.ID), url.PathEscape(action.ID))
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	output, _ := cmd.Flags().GetString("output")

	if dryRun {
		var preview struct {
			Content string `json:"content"`
		}
		if err := client.PostJSON(ctx, basePath+"/render", nil, &preview); err != nil {
			return fmt.Errorf("render quick action: %w", err)
		}
		if output == "json" {
			return cli.PrintJSON(os.Stdout, preview)
		}
		fmt.Fprintln(os.Stdout, preview.Content)
		return nil
	}

	var result map[string]any
	if err := client.PostJSON(ctx, basePath+"/run", nil, &result); err != nil {
		return fmt.Errorf("run quick action: %w", err)
	}

	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Fprintf(os.Stdout, "Ran quick action %q on %s (comment %s).\n", action.Name, args[1], strVal(result, "id"))
	if outcomes, ok := result["trigger_outcomes"].([]any); ok {
		for _, raw := range outcomes {
			outcome, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			target := strVal(outcome, "target_name")
			if target == "" {
				target = strVal(outcome, "target_id")
			}
			status := strVal(outcome, "status")
			reason := strVal(outcome, "reason_code")
			if reason != "" {
				fmt.Fprintf(os.Stdout, "  %s: %s (%s)\n", target, status, reason)
			} else {
				fmt.Fprintf(os.Stdout, "  %s: %s\n", target, status)
			}
		}
	}
	return nil
}
