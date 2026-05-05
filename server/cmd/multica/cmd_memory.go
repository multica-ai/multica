package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ---------------------------------------------------------------------------
// Memory commands — workspace-scoped CRUD for memory_artifact rows
// (wiki pages, agent notes, runbooks, decisions). Closes the agent
// write loop: runtime injection (#5) gives agents a read path; this
// gives them a write path so they can append findings as they go.
//
// Designed for agent-friendly invocation:
//   - --content-file - reads from stdin (so agents can pipe markdown)
//   - --anchor type:id is a single flag (issue, project, agent, channel)
//   - --tags a,b,c is comma-separated (no shell-quoting per tag)
//   - --output defaults to json on writes (so the agent gets the new id)
// ---------------------------------------------------------------------------

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Work with workspace memory artifacts (wiki, runbook, decision, agent note)",
	Long: `Memory artifacts are workspace-scoped, kind-discriminated markdown
content that humans curate and agents append to. Use this command to
list, search, read, write, and anchor artifacts to issues, projects,
agents, or channels — anchored artifacts are surfaced to agents at
task-claim time as part of their runtime context.`,
}

var memoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List memory artifacts in the workspace",
	RunE:  runMemoryList,
}

var memoryGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a memory artifact by id",
	Args:  exactArgs(1),
	RunE:  runMemoryGet,
}

var memorySearchCmd = &cobra.Command{
	Use:   "search",
	Short: "Full-text search memory artifacts",
	RunE:  runMemorySearch,
}

var memoryByAnchorCmd = &cobra.Command{
	Use:   "by-anchor <type> <id>",
	Short: "List artifacts anchored to a specific entity (issue|project|agent|channel)",
	Args:  exactArgs(2),
	RunE:  runMemoryByAnchor,
}

var memoryCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new memory artifact",
	RunE:  runMemoryCreate,
}

var memoryUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a memory artifact (partial — only fields you pass are changed)",
	Args:  exactArgs(1),
	RunE:  runMemoryUpdate,
}

var memoryArchiveCmd = &cobra.Command{
	Use:   "archive <id>",
	Short: "Soft-delete a memory artifact (reversible via `restore`)",
	Args:  exactArgs(1),
	RunE:  runMemoryArchive,
}

var memoryRestoreCmd = &cobra.Command{
	Use:   "restore <id>",
	Short: "Restore an archived memory artifact",
	Args:  exactArgs(1),
	RunE:  runMemoryRestore,
}

var memoryDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Hard-delete a memory artifact (irreversible — prefer `archive`)",
	Args:  exactArgs(1),
	RunE:  runMemoryDelete,
}

func init() {
	memoryCmd.AddCommand(memoryListCmd)
	memoryCmd.AddCommand(memoryGetCmd)
	memoryCmd.AddCommand(memorySearchCmd)
	memoryCmd.AddCommand(memoryByAnchorCmd)
	memoryCmd.AddCommand(memoryCreateCmd)
	memoryCmd.AddCommand(memoryUpdateCmd)
	memoryCmd.AddCommand(memoryArchiveCmd)
	memoryCmd.AddCommand(memoryRestoreCmd)
	memoryCmd.AddCommand(memoryDeleteCmd)

	// list
	memoryListCmd.Flags().String("kind", "", "Filter by kind: wiki_page | agent_note | runbook | decision")
	memoryListCmd.Flags().Bool("include-archived", false, "Include soft-deleted artifacts")
	memoryListCmd.Flags().Int("limit", 50, "Max artifacts to return (cap: 200)")
	memoryListCmd.Flags().Int("offset", 0, "Offset for pagination")
	memoryListCmd.Flags().String("output", "table", "Output format: table or json")

	// get
	memoryGetCmd.Flags().String("output", "json", "Output format: table or json")

	// search
	memorySearchCmd.Flags().String("q", "", "Search query (required) — uses websearch_to_tsquery")
	memorySearchCmd.Flags().String("kind", "", "Filter by kind")
	memorySearchCmd.Flags().Int("limit", 50, "Max results to return (cap: 200)")
	memorySearchCmd.Flags().Int("offset", 0, "Offset for pagination")
	memorySearchCmd.Flags().String("output", "table", "Output format: table or json")

	// by-anchor
	memoryByAnchorCmd.Flags().Int("limit", 50, "Max artifacts to return (cap: 200)")
	memoryByAnchorCmd.Flags().String("output", "table", "Output format: table or json")

	// create
	memoryCreateCmd.Flags().String("kind", "", "Artifact kind (required): wiki_page | agent_note | runbook | decision")
	memoryCreateCmd.Flags().String("title", "", "Artifact title (required, max 500 chars)")
	memoryCreateCmd.Flags().String("content", "", "Artifact content (markdown). Mutually exclusive with --content-file.")
	memoryCreateCmd.Flags().String("content-file", "", "Path to a file containing the artifact content. Use - for stdin.")
	memoryCreateCmd.Flags().String("anchor", "", "Anchor in `type:id` form (e.g. issue:abc123, project:def456, agent:..., channel:...)")
	memoryCreateCmd.Flags().StringSlice("tags", nil, "Comma-separated tags")
	memoryCreateCmd.Flags().String("slug", "", "Optional URL-safe slug (lowercase letters, digits, hyphens)")
	memoryCreateCmd.Flags().String("parent-id", "", "Parent artifact id for folder hierarchy")
	memoryCreateCmd.Flags().Bool("always-inject", false, "Always inject this artifact into every agent task in this workspace (workspace-wide context like deploy guides, brand rules)")
	memoryCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// update
	memoryUpdateCmd.Flags().String("title", "", "New title")
	memoryUpdateCmd.Flags().String("content", "", "New content. Mutually exclusive with --content-file.")
	memoryUpdateCmd.Flags().String("content-file", "", "Path to file with new content. Use - for stdin.")
	memoryUpdateCmd.Flags().String("anchor", "", "New anchor in `type:id` form. Pass `none` to clear the anchor.")
	memoryUpdateCmd.Flags().StringSlice("tags", nil, "Replace tags (comma-separated)")
	memoryUpdateCmd.Flags().String("slug", "", "New slug")
	memoryUpdateCmd.Flags().String("parent-id", "", "New parent id (or empty string to clear)")
	memoryUpdateCmd.Flags().Bool("always-inject", false, "Set always-inject flag to true (use --always-inject=false to clear; only takes effect when explicitly passed)")
	memoryUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	memoryArchiveCmd.Flags().String("output", "json", "Output format: table or json")
	memoryRestoreCmd.Flags().String("output", "json", "Output format: table or json")
	memoryDeleteCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// readContent resolves the content source: explicit string > file path > stdin
// (when path == "-"). Returns ("", false, nil) if neither flag is set, telling
// the caller "user didn't provide content; use existing or skip the field."
// When both --content and --content-file are set, --content wins to keep the
// behavior predictable — explicit value overrides indirection.
func readContent(cmd *cobra.Command) (content string, provided bool, err error) {
	if v, _ := cmd.Flags().GetString("content"); v != "" {
		return v, true, nil
	}
	path, _ := cmd.Flags().GetString("content-file")
	if path == "" {
		return "", false, nil
	}
	if path == "-" {
		buf, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", false, fmt.Errorf("read stdin: %w", err)
		}
		return string(buf), true, nil
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		return "", false, fmt.Errorf("read content file: %w", err)
	}
	return string(buf), true, nil
}

// parseAnchor splits "type:id" into its parts. Empty string returns
// ("", "", true, nil) meaning "no anchor specified". The literal "none"
// returns ("", "", false, nil) meaning "explicitly clear the anchor"
// (used by `update` to drop an artifact's anchor without otherwise
// changing it). Anything else must contain exactly one colon.
func parseAnchor(raw string) (anchorType, anchorID string, provided bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false, nil
	}
	if raw == "none" {
		// Sentinel — provided=true so the caller knows to send {anchor_type: null, anchor_id: null}.
		return "", "", true, nil
	}
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false, fmt.Errorf("--anchor must be `type:id` (e.g. issue:abc123) or `none` to clear")
	}
	t := strings.TrimSpace(parts[0])
	switch t {
	case "issue", "project", "agent", "channel":
		// ok
	default:
		return "", "", false, fmt.Errorf("--anchor type must be one of: issue, project, agent, channel (got %q)", t)
	}
	return t, strings.TrimSpace(parts[1]), true, nil
}

// summarizeMemoryRow produces the table row for list/search output. Tags are
// joined with `, ` and trimmed to a reasonable display width; titles are
// untruncated since they're the most valuable signal.
func summarizeMemoryRow(m map[string]any) []string {
	updated := strVal(m, "updated_at")
	if len(updated) >= 10 {
		updated = updated[:10]
	}
	anchorDisplay := "—"
	if t, _ := m["anchor_type"].(string); t != "" {
		id := strVal(m, "anchor_id")
		anchorDisplay = t + ":" + truncateID(id)
	}
	tags := ""
	if list, ok := m["tags"].([]any); ok && len(list) > 0 {
		parts := make([]string, 0, len(list))
		for _, v := range list {
			if s, ok := v.(string); ok {
				parts = append(parts, s)
			}
		}
		tags = strings.Join(parts, ", ")
	}
	return []string{
		truncateID(strVal(m, "id")),
		strVal(m, "kind"),
		strVal(m, "title"),
		anchorDisplay,
		tags,
		updated,
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func runMemoryList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := url.Values{}
	if k, _ := cmd.Flags().GetString("kind"); k != "" {
		params.Set("kind", k)
	}
	if includeArchived, _ := cmd.Flags().GetBool("include-archived"); includeArchived {
		params.Set("include_archived", "true")
	}
	if l, _ := cmd.Flags().GetInt("limit"); l > 0 {
		params.Set("limit", fmt.Sprintf("%d", l))
	}
	if o, _ := cmd.Flags().GetInt("offset"); o > 0 {
		params.Set("offset", fmt.Sprintf("%d", o))
	}

	path := "/api/memory"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list memory artifacts: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	listRaw, _ := result["memory_artifacts"].([]any)
	headers := []string{"ID", "KIND", "TITLE", "ANCHOR", "TAGS", "UPDATED"}
	rows := make([][]string, 0, len(listRaw))
	for _, raw := range listRaw {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, summarizeMemoryRow(m))
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runMemoryGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var artifact map[string]any
	if err := client.GetJSON(ctx, "/api/memory/"+args[0], &artifact); err != nil {
		return fmt.Errorf("get memory artifact: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		// Table output is for the index views; for `get` always default to
		// JSON since the consumer almost always wants the full content body
		// rather than a one-line summary.
		return cli.PrintJSON(os.Stdout, artifact)
	}
	return cli.PrintJSON(os.Stdout, artifact)
}

func runMemorySearch(cmd *cobra.Command, _ []string) error {
	q, _ := cmd.Flags().GetString("q")
	if q == "" {
		return fmt.Errorf("--q is required")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("q", q)
	if k, _ := cmd.Flags().GetString("kind"); k != "" {
		params.Set("kind", k)
	}
	if l, _ := cmd.Flags().GetInt("limit"); l > 0 {
		params.Set("limit", fmt.Sprintf("%d", l))
	}
	if o, _ := cmd.Flags().GetInt("offset"); o > 0 {
		params.Set("offset", fmt.Sprintf("%d", o))
	}

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/memory/search?"+params.Encode(), &result); err != nil {
		return fmt.Errorf("search memory artifacts: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	listRaw, _ := result["memory_artifacts"].([]any)
	headers := []string{"ID", "KIND", "TITLE", "ANCHOR", "TAGS", "UPDATED"}
	rows := make([][]string, 0, len(listRaw))
	for _, raw := range listRaw {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, summarizeMemoryRow(m))
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runMemoryByAnchor(cmd *cobra.Command, args []string) error {
	anchorType, anchorID := args[0], args[1]
	switch anchorType {
	case "issue", "project", "agent", "channel":
		// ok
	default:
		return fmt.Errorf("anchor type must be one of: issue, project, agent, channel (got %q)", anchorType)
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := url.Values{}
	if l, _ := cmd.Flags().GetInt("limit"); l > 0 {
		params.Set("limit", fmt.Sprintf("%d", l))
	}
	path := fmt.Sprintf("/api/memory/by-anchor/%s/%s", anchorType, anchorID)
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list memory artifacts by anchor: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	listRaw, _ := result["memory_artifacts"].([]any)
	headers := []string{"ID", "KIND", "TITLE", "TAGS", "UPDATED"}
	rows := make([][]string, 0, len(listRaw))
	for _, raw := range listRaw {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		updated := strVal(m, "updated_at")
		if len(updated) >= 10 {
			updated = updated[:10]
		}
		tags := ""
		if list, ok := m["tags"].([]any); ok && len(list) > 0 {
			parts := make([]string, 0, len(list))
			for _, v := range list {
				if s, ok := v.(string); ok {
					parts = append(parts, s)
				}
			}
			tags = strings.Join(parts, ", ")
		}
		rows = append(rows, []string{
			truncateID(strVal(m, "id")),
			strVal(m, "kind"),
			strVal(m, "title"),
			tags,
			updated,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runMemoryCreate(cmd *cobra.Command, _ []string) error {
	kind, _ := cmd.Flags().GetString("kind")
	title, _ := cmd.Flags().GetString("title")
	if kind == "" {
		return fmt.Errorf("--kind is required (wiki_page | agent_note | runbook | decision)")
	}
	if title == "" {
		return fmt.Errorf("--title is required")
	}

	content, _, err := readContent(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{
		"kind":    kind,
		"title":   title,
		"content": content,
	}

	if anchorRaw, _ := cmd.Flags().GetString("anchor"); anchorRaw != "" {
		t, id, _, err := parseAnchor(anchorRaw)
		if err != nil {
			return err
		}
		if t != "" {
			body["anchor_type"] = t
			body["anchor_id"] = id
		}
		// Note: "none" is meaningless on create (an unset anchor is the default
		// for create); we silently ignore the sentinel rather than erroring.
	}
	if tags, _ := cmd.Flags().GetStringSlice("tags"); len(tags) > 0 {
		body["tags"] = tags
	}
	if slug, _ := cmd.Flags().GetString("slug"); slug != "" {
		body["slug"] = slug
	}
	if parentID, _ := cmd.Flags().GetString("parent-id"); parentID != "" {
		body["parent_id"] = parentID
	}
	if cmd.Flags().Changed("always-inject") {
		// Only send the field when the user explicitly opted in/out so the
		// server-side default (`false`) takes effect when the flag isn't passed.
		v, _ := cmd.Flags().GetBool("always-inject")
		body["always_inject_at_runtime"] = v
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/memory", body, &result); err != nil {
		return fmt.Errorf("create memory artifact: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "KIND", "TITLE", "ANCHOR"}
		anchorDisplay := "—"
		if t, _ := result["anchor_type"].(string); t != "" {
			anchorDisplay = t + ":" + truncateID(strVal(result, "anchor_id"))
		}
		rows := [][]string{{
			truncateID(strVal(result, "id")),
			strVal(result, "kind"),
			strVal(result, "title"),
			anchorDisplay,
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runMemoryUpdate(cmd *cobra.Command, args []string) error {
	body := map[string]any{}

	if v, _ := cmd.Flags().GetString("title"); v != "" {
		body["title"] = v
	}
	if content, provided, err := readContent(cmd); err != nil {
		return err
	} else if provided {
		body["content"] = content
	}
	if anchorRaw, _ := cmd.Flags().GetString("anchor"); anchorRaw != "" {
		t, id, _, err := parseAnchor(anchorRaw)
		if err != nil {
			return err
		}
		if anchorRaw == "none" {
			body["anchor_type"] = nil
			body["anchor_id"] = nil
		} else {
			body["anchor_type"] = t
			body["anchor_id"] = id
		}
	}
	// StringSlice's Changed flag is the only signal that the user passed
	// --tags, since an empty slice could mean "unset" or "not given." We
	// honor `--tags ""` as "clear all tags" via Changed.
	if cmd.Flags().Changed("tags") {
		tags, _ := cmd.Flags().GetStringSlice("tags")
		if tags == nil {
			tags = []string{}
		}
		body["tags"] = tags
	}
	if v, _ := cmd.Flags().GetString("slug"); v != "" {
		body["slug"] = v
	}
	if cmd.Flags().Changed("parent-id") {
		body["parent_id"] = func() any {
			v, _ := cmd.Flags().GetString("parent-id")
			if v == "" {
				return nil
			}
			return v
		}()
	}
	if cmd.Flags().Changed("always-inject") {
		v, _ := cmd.Flags().GetBool("always-inject")
		body["always_inject_at_runtime"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("nothing to update — pass at least one of --title, --content, --content-file, --anchor, --tags, --slug, --parent-id, --always-inject")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/memory/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update memory artifact: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "KIND", "TITLE", "ANCHOR"}
		anchorDisplay := "—"
		if t, _ := result["anchor_type"].(string); t != "" {
			anchorDisplay = t + ":" + truncateID(strVal(result, "anchor_id"))
		}
		rows := [][]string{{
			truncateID(strVal(result, "id")),
			strVal(result, "kind"),
			strVal(result, "title"),
			anchorDisplay,
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runMemoryArchive(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/memory/"+args[0]+"/archive", map[string]any{}, &result); err != nil {
		return fmt.Errorf("archive memory artifact: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Fprintf(os.Stdout, "Memory artifact %s archived.\n", args[0])
	return nil
}

func runMemoryRestore(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/memory/"+args[0]+"/restore", map[string]any{}, &result); err != nil {
		return fmt.Errorf("restore memory artifact: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Fprintf(os.Stdout, "Memory artifact %s restored.\n", args[0])
	return nil
}

func runMemoryDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/memory/"+args[0]); err != nil {
		return fmt.Errorf("delete memory artifact: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"id": args[0], "deleted": true})
	}
	fmt.Fprintf(os.Stdout, "Memory artifact %s deleted.\n", args[0])
	return nil
}
