package main

// CEREBRO-PATCH(notifications-cli): JEH-1807 notification center CLI.

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var notificationsCmd = &cobra.Command{
	Use:   "notifications",
	Short: "Work with notifications",
}

var notificationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your notifications",
	RunE:  runNotificationsList,
}

func init() {
	notificationsCmd.AddCommand(notificationsListCmd)
	registerNotificationsListFlags(notificationsListCmd)
}

func registerNotificationsListFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("unread", false, "Only show unread notifications")
	cmd.Flags().Int("limit", 50, "Maximum number of notifications to return")
	cmd.Flags().String("output", "table", "Output format: table or json")
}

func runNotificationsList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	limit, _ := cmd.Flags().GetInt("limit")
	if limit <= 0 {
		return fmt.Errorf("--limit must be greater than 0")
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	if unread, _ := cmd.Flags().GetBool("unread"); unread {
		params.Set("unread", "true")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/notifications?" + params.Encode()
	var result any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list notifications: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	switch output {
	case "json":
		return cli.PrintJSON(os.Stdout, result)
	case "table":
		notifications := notificationRowsFromResult(result)
		headers := []string{"TYPE", "ISSUE TITLE", "EXCERPT", "CREATED_AT", "READ_AT"}
		rows := make([][]string, 0, len(notifications))
		for _, n := range notifications {
			rows = append(rows, []string{
				strVal(n, "type"),
				firstNonEmpty(strVal(n, "issue_title"), strVal(n, "title")),
				truncateRunes(firstNonEmpty(strVal(n, "excerpt"), strVal(n, "body")), 80),
				strVal(n, "created_at"),
				strVal(n, "read_at"),
			})
		}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	default:
		return fmt.Errorf("unsupported --output %q (use table or json)", output)
	}
}

func notificationRowsFromResult(result any) []map[string]any {
	switch v := result.(type) {
	case []any:
		return mapsFromAnySlice(v)
	case map[string]any:
		for _, key := range []string{"notifications", "items", "data"} {
			if rows, ok := v[key].([]any); ok {
				return mapsFromAnySlice(rows)
			}
		}
	}
	return nil
}

func mapsFromAnySlice(items []any) []map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, raw := range items {
		if row, ok := raw.(map[string]any); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
