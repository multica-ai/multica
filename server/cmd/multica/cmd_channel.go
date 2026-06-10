package main

// CEREBRO-PATCH(fir-125-channel-cli): multica channel list — exposes GET /api/channels
// CEREBRO-PATCH(tech-3255-channel-kind-filter): --kind flag to filter channels vs DMs.
// CEREBRO-PATCH(tech-3255-channel-messages): multica channel messages — GET /api/issues/{id}/comments.

import (
	"context"
	"fmt"
	"os"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Work with channels and DMs",
}

var channelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List channels and DMs",
	RunE:  runChannelList,
}

// CEREBRO-PATCH(tech-3255-channel-messages): list messages in a channel or DM.
var channelMessagesCmd = &cobra.Command{
	Use:   "messages <channel-id>",
	Short: "List messages in a channel or DM",
	Args:  exactArgs(1),
	RunE:  runChannelMessages,
}

func init() {
	channelCmd.AddCommand(channelListCmd)
	channelCmd.AddCommand(channelMessagesCmd) // CEREBRO-PATCH(tech-3255-channel-messages)

	channelListCmd.Flags().String("output", "table", "Output format: table or json")
	channelListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	channelListCmd.Flags().Bool("all", false, "List all channels in workspace (not just your own)")
	channelListCmd.Flags().String("kind", "", "Filter by kind: channel or dm (default: both)") // CEREBRO-PATCH(tech-3255-channel-kind-filter)

	// CEREBRO-PATCH(tech-3255-channel-messages): channel messages flags.
	channelMessagesCmd.Flags().String("output", "table", "Output format: table or json")
	channelMessagesCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	channelMessagesCmd.Flags().String("since", "", "Only return messages after this RFC3339 timestamp")
	channelMessagesCmd.Flags().Int("limit", 100, "Maximum number of messages to return (maps to --recent on the server)")
}

func runChannelList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := "/api/channels"
	if all, _ := cmd.Flags().GetBool("all"); all {
		path += "?all=true"
	}

	var result []any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list channels: %w", err)
	}

	// CEREBRO-PATCH(tech-3255-channel-kind-filter): filter by kind after fetching.
	kind, _ := cmd.Flags().GetString("kind")
	if kind != "" {
		filtered := make([]any, 0, len(result))
		for _, raw := range result {
			ch, ok := raw.(map[string]any)
			if ok && strVal(ch, "kind") == kind {
				filtered = append(filtered, ch)
			}
		}
		result = filtered
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "KIND", "TITLE", "CREATOR", "PARTICIPANTS", "UPDATED"}
	rows := make([][]string, 0, len(result))
	for _, raw := range result {
		ch, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		parts, _ := ch["participants"].([]any)
		updated := strVal(ch, "updated_at")
		if len(updated) >= 10 {
			updated = updated[:10]
		}
		rows = append(rows, []string{
			displayID(strVal(ch, "id"), fullID),
			strVal(ch, "kind"),
			strVal(ch, "title"),
			displayID(strVal(ch, "creator_id"), fullID),
			fmt.Sprintf("%d", len(parts)),
			updated,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// CEREBRO-PATCH(tech-3255-channel-messages): runChannelMessages lists comments
// for a channel or DM issue. Channels are stored as issues; their messages are
// issue comments at /api/issues/{id}/comments.
func runChannelMessages(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	channelID := args[0]
	params := ""
	if since, _ := cmd.Flags().GetString("since"); since != "" {
		params = "?since=" + since
	}
	if limit, _ := cmd.Flags().GetInt("limit"); limit > 0 && params == "" {
		params = fmt.Sprintf("?recent=%d", limit)
	} else if limit > 0 {
		params += fmt.Sprintf("&recent=%d", limit)
	}

	var messages []map[string]any
	if err := client.GetJSON(ctx, "/api/issues/"+channelID+"/comments"+params, &messages); err != nil {
		return fmt.Errorf("list messages: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, messages)
	}

	actors := loadActorDisplayLookup(ctx, client)
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "AUTHOR", "CONTENT", "CREATED"}
	rows := make([][]string, 0, len(messages))
	for _, m := range messages {
		content := strVal(m, "content")
		if utf8.RuneCountInString(content) > 80 {
			runes := []rune(content)
			content = string(runes[:77]) + "..."
		}
		created := strVal(m, "created_at")
		if len(created) >= 16 {
			created = created[:16]
		}
		rows = append(rows, []string{
			displayID(strVal(m, "id"), fullID),
			actors.actor(strVal(m, "author_type"), strVal(m, "author_id")),
			content,
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func int64OrZero(m map[string]any, key string) int64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	}
	return 0
}
