package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Work with workspace memory",
}

var memoryListCmd = &cobra.Command{
	Use:   "list",
	Short: "List memories in the workspace",
	RunE:  runMemoryList,
}

var memoryGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get memory details",
	Args:  exactArgs(1),
	RunE:  runMemoryGet,
}

var memorySearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search memory title and content",
	Args:  exactArgs(1),
	RunE:  runMemorySearch,
}

var memoryProposeCmd = &cobra.Command{
	Use:   "propose",
	Short: "Propose a memory for review",
	RunE:  runMemoryPropose,
}

var memoryApproveCmd = &cobra.Command{
	Use:   "approve <id>",
	Short: "Approve a proposed memory",
	Args:  exactArgs(1),
	RunE:  runMemoryApprove,
}

var memoryRejectCmd = &cobra.Command{
	Use:   "reject <id>",
	Short: "Reject a proposed memory",
	Args:  exactArgs(1),
	RunE:  runMemoryReject,
}

func init() {
	memoryCmd.AddCommand(memoryListCmd)
	memoryCmd.AddCommand(memoryGetCmd)
	memoryCmd.AddCommand(memorySearchCmd)
	memoryCmd.AddCommand(memoryProposeCmd)
	memoryCmd.AddCommand(memoryApproveCmd)
	memoryCmd.AddCommand(memoryRejectCmd)

	for _, cmd := range []*cobra.Command{memoryListCmd, memorySearchCmd} {
		cmd.Flags().String("status", "", "Filter by status: pending, approved, or rejected")
		cmd.Flags().String("scope", "", "Filter by scope: workspace, project, agent, or issue")
		cmd.Flags().String("scope-id", "", "Filter by scope ID")
		cmd.Flags().Int("limit", 50, "Maximum number of memories to return")
		cmd.Flags().Int("offset", 0, "Number of memories to skip")
		cmd.Flags().String("output", "table", "Output format: table or json")
	}

	memoryGetCmd.Flags().String("output", "json", "Output format: table or json")

	memoryProposeCmd.Flags().String("title", "", "Memory title (required)")
	memoryProposeCmd.Flags().String("content", "", "Memory content")
	memoryProposeCmd.Flags().Bool("content-stdin", false, "Read memory content from stdin")
	memoryProposeCmd.Flags().String("scope", "workspace", "Memory scope: workspace, project, agent, or issue")
	memoryProposeCmd.Flags().String("scope-id", "", "Scope ID for project, agent, or issue memory")
	memoryProposeCmd.Flags().String("source-issue", "", "Source issue ID or identifier")
	memoryProposeCmd.Flags().String("source-comment", "", "Source comment ID")
	memoryProposeCmd.Flags().String("output", "json", "Output format: table or json")

	memoryApproveCmd.Flags().String("note", "", "Review note")
	memoryApproveCmd.Flags().String("output", "json", "Output format: table or json")

	memoryRejectCmd.Flags().String("note", "", "Review note")
	memoryRejectCmd.Flags().String("output", "json", "Output format: table or json")
}

func runMemoryList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/memories"
	if params := memoryFilterParams(cmd, ""); len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list memories: %w", err)
	}
	return printMemoryCollection(cmd, result)
}

func runMemoryGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var memory map[string]any
	if err := client.GetJSON(ctx, "/api/memories/"+args[0], &memory); err != nil {
		return fmt.Errorf("get memory: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printMemoryTable([]map[string]any{memory}, true)
		return nil
	}
	return cli.PrintJSON(os.Stdout, memory)
}

func runMemorySearch(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/memories/search?" + memoryFilterParams(cmd, args[0]).Encode()
	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("search memories: %w", err)
	}
	return printMemoryCollection(cmd, result)
}

func runMemoryPropose(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Errorf("--title is required")
	}

	content, err := memoryContentFromFlags(cmd)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("--content is required unless --content-stdin is used")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	scope, _ := cmd.Flags().GetString("scope")
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "workspace"
	}

	body := map[string]any{
		"title":      title,
		"content":    strings.TrimSpace(content),
		"scope_type": scope,
	}
	if scopeID, _ := cmd.Flags().GetString("scope-id"); strings.TrimSpace(scopeID) != "" {
		body["scope_id"] = strings.TrimSpace(scopeID)
	}
	if sourceIssue, _ := cmd.Flags().GetString("source-issue"); strings.TrimSpace(sourceIssue) != "" {
		body["source_issue_id"] = strings.TrimSpace(sourceIssue)
		if scope == "issue" {
			if _, hasScope := body["scope_id"]; !hasScope {
				body["scope_id"] = strings.TrimSpace(sourceIssue)
			}
		}
	}
	if sourceComment, _ := cmd.Flags().GetString("source-comment"); strings.TrimSpace(sourceComment) != "" {
		body["source_comment_id"] = strings.TrimSpace(sourceComment)
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/memories/propose", body, &result); err != nil {
		return fmt.Errorf("propose memory: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Memory proposed: %s.\n", truncateID(strVal(result, "id")))
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printMemoryTable([]map[string]any{result}, false)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runMemoryApprove(cmd *cobra.Command, args []string) error {
	return runMemoryReview(cmd, args[0], "approve")
}

func runMemoryReject(cmd *cobra.Command, args []string) error {
	return runMemoryReview(cmd, args[0], "reject")
}

func runMemoryReview(cmd *cobra.Command, id, action string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	note, _ := cmd.Flags().GetString("note")
	body := map[string]any{}
	if strings.TrimSpace(note) != "" {
		body["note"] = strings.TrimSpace(note)
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/memories/"+id+"/"+action, body, &result); err != nil {
		return fmt.Errorf("%s memory: %w", action, err)
	}

	past := "approved"
	if action == "reject" {
		past = "rejected"
	}
	fmt.Fprintf(os.Stderr, "Memory %s: %s.\n", past, truncateID(id))
	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printMemoryTable([]map[string]any{result}, false)
		return nil
	}
	return cli.PrintJSON(os.Stdout, result)
}

func memoryFilterParams(cmd *cobra.Command, query string) url.Values {
	params := url.Values{}
	if query != "" {
		params.Set("q", query)
	}
	if v, _ := cmd.Flags().GetString("status"); strings.TrimSpace(v) != "" {
		params.Set("status", strings.TrimSpace(v))
	}
	if v, _ := cmd.Flags().GetString("scope"); strings.TrimSpace(v) != "" {
		params.Set("scope_type", strings.TrimSpace(v))
	}
	if v, _ := cmd.Flags().GetString("scope-id"); strings.TrimSpace(v) != "" {
		params.Set("scope_id", strings.TrimSpace(v))
	}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetInt("offset"); v > 0 {
		params.Set("offset", fmt.Sprintf("%d", v))
	}
	return params
}

func memoryContentFromFlags(cmd *cobra.Command) (string, error) {
	content, _ := cmd.Flags().GetString("content")
	fromStdin, _ := cmd.Flags().GetBool("content-stdin")
	if !fromStdin {
		return content, nil
	}
	if content != "" {
		return "", fmt.Errorf("--content and --content-stdin are mutually exclusive")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return string(data), nil
}

func printMemoryCollection(cmd *cobra.Command, result map[string]any) error {
	memoriesRaw, _ := result["memories"].([]any)
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	memories := make([]map[string]any, 0, len(memoriesRaw))
	for _, raw := range memoriesRaw {
		if memory, ok := raw.(map[string]any); ok {
			memories = append(memories, memory)
		}
	}
	printMemoryTable(memories, false)
	return nil
}

func printMemoryTable(memories []map[string]any, includeContent bool) {
	headers := []string{"ID", "SCOPE", "STATUS", "TITLE", "SOURCE", "UPDATED"}
	if includeContent {
		headers = append(headers, "CONTENT")
	}

	rows := make([][]string, 0, len(memories))
	for _, memory := range memories {
		updated := strVal(memory, "updated_at")
		if len(updated) >= 16 {
			updated = updated[:16]
		}
		row := []string{
			truncateID(strVal(memory, "id")),
			formatMemoryScope(memory),
			strVal(memory, "status"),
			truncateRunes(strVal(memory, "title"), 48),
			formatMemorySource(memory),
			updated,
		}
		if includeContent {
			row = append(row, truncateRunes(strVal(memory, "content"), 120))
		}
		rows = append(rows, row)
	}
	cli.PrintTable(os.Stdout, headers, rows)
}

func formatMemoryScope(memory map[string]any) string {
	scope := strVal(memory, "scope_type")
	scopeID := strVal(memory, "scope_id")
	if scopeID == "" {
		return scope
	}
	return scope + ":" + truncateID(scopeID)
}

func formatMemorySource(memory map[string]any) string {
	if commentID := strVal(memory, "source_comment_id"); commentID != "" {
		return "comment:" + truncateID(commentID)
	}
	if issueID := strVal(memory, "source_issue_id"); issueID != "" {
		return "issue:" + truncateID(issueID)
	}
	return ""
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
