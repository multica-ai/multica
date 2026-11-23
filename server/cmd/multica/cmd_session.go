package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// sessionListMaxPageSize mirrors chatSessionsPageMaxLimit in the chat handler.
const sessionListMaxPageSize = 100

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "List and manage workspace chat sessions",
	Long: `Workspace chat sessions (conversations), distinct from channel transcript reads.

Use "multica chat history" and "multica chat thread" to read the external channel
transcript for the single conversation you are currently running in. "multica
session list" lists your Multica chat sessions in the workspace — metadata only,
with pagination and activity filters — so agents and automation can discover stale
conversations without reading message bodies.`,
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List chat sessions in the workspace",
	Long: `List the caller's chat sessions in the current workspace (metadata only).

Filters:
  --status active|archived|all   Session lifecycle filter (default: active)
  --since <RFC3339>              Last activity at or after this time
  --before <RFC3339>             Last activity strictly before this time

Pagination uses --limit and --offset against GET /api/chat/sessions/page.
Default output is JSON; use --output table for a compact summary.`,
	RunE: runSessionList,
}

func init() {
	sessionListCmd.Flags().String("output", "json", "Output format: table or json")
	sessionListCmd.Flags().String("status", "active", "Filter by session status: active, archived, or all")
	sessionListCmd.Flags().String("since", "", "Include sessions whose last activity is at or after this RFC3339 timestamp")
	sessionListCmd.Flags().String("before", "", "Include sessions whose last activity is strictly before this RFC3339 timestamp")
	sessionListCmd.Flags().Int("limit", 50, fmt.Sprintf("Page size, 1 to %d (use --offset to read further pages)", sessionListMaxPageSize))
	sessionListCmd.Flags().Int("offset", 0, "Number of sessions to skip")
	sessionCmd.AddCommand(sessionListCmd)
}

func runSessionList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if client.WorkspaceID == "" {
		if _, err := requireWorkspaceID(cmd); err != nil {
			return err
		}
	}

	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 1 || limit > sessionListMaxPageSize {
		return fmt.Errorf("--limit must be between 1 and %d", sessionListMaxPageSize)
	}
	offset, _ := cmd.Flags().GetInt("offset")
	if offset < 0 {
		return fmt.Errorf("--offset must be zero or greater")
	}

	status, _ := cmd.Flags().GetString("status")
	switch status {
	case "active", "archived", "all":
	default:
		return fmt.Errorf("--status must be active, archived, or all")
	}

	params := url.Values{}
	params.Set("status", status)
	params.Set("limit", fmt.Sprintf("%d", limit))
	if offset > 0 {
		params.Set("offset", fmt.Sprintf("%d", offset))
	}
	if since, _ := cmd.Flags().GetString("since"); since != "" {
		if _, err := time.Parse(time.RFC3339Nano, since); err != nil {
			if _, err2 := time.Parse(time.RFC3339, since); err2 != nil {
				return fmt.Errorf("invalid --since; expected an RFC3339 timestamp: %w", err)
			}
		}
		params.Set("activity_since", since)
	}
	if before, _ := cmd.Flags().GetString("before"); before != "" {
		if _, err := time.Parse(time.RFC3339Nano, before); err != nil {
			if _, err2 := time.Parse(time.RFC3339, before); err2 != nil {
				return fmt.Errorf("invalid --before; expected an RFC3339 timestamp: %w", err)
			}
		}
		params.Set("activity_before", before)
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/chat/sessions/page?"+params.Encode(), &result); err != nil {
		return fmt.Errorf("list chat sessions: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output != "table" {
		return cli.PrintJSON(os.Stdout, result)
	}

	sessions, _ := result["sessions"].([]any)
	headers := []string{"ID", "TITLE", "STATUS", "AGENT", "LAST_ACTIVITY", "CHANNEL"}
	rows := make([][]string, 0, len(sessions))
	for _, raw := range sessions {
		s, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		channel := ""
		if src, ok := s["channel_source"].(map[string]any); ok {
			channel = strVal(src, "channel_type")
		}
		rows = append(rows, []string{
			strVal(s, "id"),
			strVal(s, "title"),
			strVal(s, "status"),
			strVal(s, "agent_id"),
			strVal(s, "last_activity_at"),
			channel,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	total := strVal(result, "total")
	hasMore := strVal(result, "has_more")
	fmt.Fprintf(os.Stdout, "\nTotal: %s  has_more: %s  limit: %s  offset: %s\n",
		total, hasMore, strVal(result, "limit"), strVal(result, "offset"))
	return nil
}
