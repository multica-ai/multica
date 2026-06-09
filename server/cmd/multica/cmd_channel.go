package main

// CEREBRO-PATCH(fir-125-channel-cli): multica channel list — exposes GET /api/channels

import (
	"context"
	"fmt"
	"os"
	"time"

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

func init() {
	channelCmd.AddCommand(channelListCmd)
	channelListCmd.Flags().String("output", "table", "Output format: table or json")
	channelListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	channelListCmd.Flags().Bool("all", false, "List all channels in workspace (not just your own)")
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
