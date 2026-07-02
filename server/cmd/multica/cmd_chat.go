package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Work with the current chat conversation",
}

var chatHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Read prior messages from the chat channel this conversation came from",
	Long: `Read the earlier messages of the chat channel (e.g. a Slack thread, channel,
or DM) that this conversation is connected to.

When you are @mentioned in a Slack thread or channel you only receive the one
triggering message — not what was said before it. Run this to pull the
surrounding conversation so you understand the full context.

A conversation has two nested histories: the surrounding CHANNEL and your own
THREAD within it (your first reply opens a thread on the @mention). By default
(--scope auto) the server reads the channel on your first reply — where the
prior context lives — and your thread on follow-ups. Use --scope channel to pull
the wider channel during a follow-up when the thread alone is not enough, or
--scope thread to force the thread.

It is the SAME command regardless of which channel the conversation came from;
the server hides the per-platform differences. It reads only the conversation
you are currently running for — it cannot read any other session or channel.`,
	Args: cobra.NoArgs,
	RunE: runChatHistory,
}

func init() {
	chatHistoryCmd.Flags().String("scope", "auto", "Which history to read: auto, thread, or channel")
	chatHistoryCmd.Flags().Int("limit", 0, "Maximum number of messages to return (the server clamps the range)")
	chatHistoryCmd.Flags().String("before", "", "Opaque cursor (a next_cursor from a prior page) to read older messages")
	chatHistoryCmd.Flags().String("output", "json", "Output format: table or json")
	chatCmd.AddCommand(chatHistoryCmd)
}

func runChatHistory(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	scope, _ := cmd.Flags().GetString("scope")
	limit, _ := cmd.Flags().GetInt("limit")
	before, _ := cmd.Flags().GetString("before")

	q := url.Values{}
	if scope != "" && scope != "auto" {
		q.Set("scope", scope)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if before != "" {
		q.Set("before", before)
	}
	path := "/api/chat/history"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var resp map[string]any
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("read chat history: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		if note := strVal(resp, "note"); note != "" {
			fmt.Fprintln(os.Stdout, note)
			return nil
		}
		if s := strVal(resp, "scope"); s != "" {
			fmt.Fprintf(os.Stdout, "scope: %s\n", s)
		}
		msgs, _ := resp["messages"].([]any)
		headers := []string{"TS", "ROLE", "AUTHOR", "TEXT"}
		rows := make([][]string, 0, len(msgs))
		for _, mi := range msgs {
			m, ok := mi.(map[string]any)
			if !ok {
				continue
			}
			rows = append(rows, []string{strVal(m, "ts"), strVal(m, "role"), strVal(m, "author"), strVal(m, "text")})
		}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, resp)
}
