package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ---------------------------------------------------------------------------
// Channel commands — read-only access to channel message history. Agents
// triggered by an @mention get the last 50 messages auto-injected into
// their context; this CLI exists for the case where the agent needs to
// look further back to ground its reply.
// ---------------------------------------------------------------------------

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Read channel message history",
}

var channelHistoryCmd = &cobra.Command{
	Use:   "history <channel-id>",
	Short: "List messages in a channel, paginated by created_at",
	Long: `List messages in a channel, newest first, paginated by created_at.

The mention-triggered agent context already includes the most recent
window of messages. Use this command only when you need to go further
back — pass the oldest 'created_at' you've already seen as --before
to fetch the next page.`,
	Args: exactArgs(1),
	RunE: runChannelHistory,
}

func init() {
	channelCmd.AddCommand(channelHistoryCmd)

	channelHistoryCmd.Flags().Int("limit", 50, "Number of messages to return (1..200)")
	channelHistoryCmd.Flags().String("before", "", "RFC3339 timestamp; only returns messages strictly older")
	channelHistoryCmd.Flags().Bool("include-threaded", false, "Include thread replies in the stream")
	channelHistoryCmd.Flags().String("output", "json", "Output format: table or json")
}

func runChannelHistory(cmd *cobra.Command, args []string) error {
	channelID := strings.TrimSpace(args[0])
	if channelID == "" {
		return fmt.Errorf("channel id is required")
	}

	limit, _ := cmd.Flags().GetInt("limit")
	before, _ := cmd.Flags().GetString("before")
	includeThreaded, _ := cmd.Flags().GetBool("include-threaded")
	output, _ := cmd.Flags().GetString("output")

	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	if before != "" {
		if _, err := time.Parse(time.RFC3339, before); err != nil {
			return fmt.Errorf("--before must be RFC3339 (e.g. 2026-04-30T12:00:00Z): %w", err)
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	if before != "" {
		params.Set("before", before)
	}
	if includeThreaded {
		params.Set("include_threaded", "true")
	}
	path := "/api/channels/" + channelID + "/messages?" + params.Encode()

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list channel messages: %w", err)
	}

	messagesRaw, _ := result["messages"].([]any)

	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	headers := []string{"CREATED", "AUTHOR", "ID", "CONTENT"}
	rows := make([][]string, 0, len(messagesRaw))
	for _, raw := range messagesRaw {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		created := strVal(m, "created_at")
		if len(created) >= 19 {
			created = created[:19] + "Z"
		}
		author := strVal(m, "author_type")
		if id := strVal(m, "author_id"); id != "" {
			author += ":" + truncateID(id)
		}
		content := strVal(m, "content")
		// Single-line preview so the table stays readable; full content
		// remains available via --output json.
		content = strings.ReplaceAll(content, "\n", " ")
		if len(content) > 80 {
			content = content[:77] + "..."
		}
		rows = append(rows, []string{
			created,
			author,
			truncateID(strVal(m, "id")),
			content,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
