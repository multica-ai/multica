package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/util"
)

func init() {
	chatCmd.AddCommand(newChatListCmd(), newChatSendCmd())
}

func newChatListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "list", Short: "List accessible active chat sessions in the current workspace",
		Long: `List active chat sessions owned by the authenticated user in the current
workspace. Inside an agent task, that user is the runtime owner bound to the task
token, not necessarily the person who initiated the task. Server-side agent
access checks still apply. This does not list other workspace members' chats.

Use the full session UUID in the output with "multica chat send". --title filters
by exact title; it never selects a destination implicitly.`,
		Args: cobra.NoArgs, RunE: runChatList,
	}
	cmd.Flags().String("title", "", "Filter by exact session title")
	cmd.Flags().String("output", "json", "Output format: json or table (table IDs are full UUIDs)")
	return cmd
}

func newChatSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "send <session-id>", Short: "Send a message to an existing chat session and enqueue a run",
		Long: `Send one text message to an existing active chat session using its full UUID.
The server checks workspace membership, session ownership and agent invocation
permissions. It persists the message and enqueues a run; if the session already
has work in flight, the new turn queues behind it. The response contains the
message_id, task_id (run ID), and queued flag. Acknowledgement is not completion.

This uses the same direct-chat input as the web composer, not an agent-authored
message card. It does not create a chat or an issue, wait for a reply, or retry.
After a server error, timeout or lost response, check the destination before sending again:
the server may already have accepted the message. Task credentials never fall
back to a saved member profile.`,
		Example: `  multica chat list --title "Review" --output json
  multica chat send <session-id> --body "Please review the proposed change."`,
		Args: exactArgs(1), RunE: runChatSend,
	}
	cmd.Flags().String("body", "", "Text message (required)")
	cmd.Flags().String("output", "json", "Output format: json or table")
	return cmd
}

func chatSessionOutput(cmd *cobra.Command) (string, error) {
	output, err := cmd.Flags().GetString("output")
	if err != nil {
		return "", err
	}
	if output != "json" && output != "table" {
		return "", fmt.Errorf("invalid output format %q: use json or table", output)
	}
	return output, nil
}

func runChatList(cmd *cobra.Command, _ []string) error {
	output, err := chatSessionOutput(cmd)
	if err != nil {
		return err
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var sessions []map[string]any
	if err := client.GetJSON(ctx, "/api/chat/sessions/", &sessions); err != nil {
		return fmt.Errorf("list chat sessions: %w", err)
	}
	title, _ := cmd.Flags().GetString("title")
	filtered := make([]map[string]any, 0, len(sessions))
	for _, session := range sessions {
		if !cmd.Flags().Changed("title") || strVal(session, "title") == title {
			filtered = append(filtered, session)
		}
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, filtered)
	}
	rows := make([][]string, 0, len(filtered))
	for _, session := range filtered {
		rows = append(rows, []string{strVal(session, "id"), strVal(session, "title"), strVal(session, "agent_id"), strVal(session, "status")})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "TITLE", "AGENT_ID", "STATUS"}, rows)
	return nil
}

func runChatSend(cmd *cobra.Command, args []string) error {
	output, err := chatSessionOutput(cmd)
	if err != nil {
		return err
	}
	sessionID, err := util.ParseUUID(args[0])
	if err != nil {
		return fmt.Errorf("session-id must be a full UUID: %w", err)
	}
	body, _ := cmd.Flags().GetString("body")
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("--body is required and must contain text")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	var result map[string]any
	path := "/api/chat/sessions/" + util.UUIDToString(sessionID) + "/messages"
	if err := client.PostJSON(ctx, path, map[string]any{"content": body}, &result); err != nil {
		var httpErr *cli.HTTPError
		if !errors.As(err, &httpErr) || httpErr.StatusCode >= http.StatusInternalServerError {
			return cli.WithUserMessage("Delivery may have succeeded. Check the destination before sending again; this command did not retry.", err)
		}
		return fmt.Errorf("send chat message: %w", err)
	}
	if strVal(result, "message_id") == "" || strVal(result, "task_id") == "" {
		return fmt.Errorf("message may have been accepted, but the response lacks message_id or task_id; check the destination before sending again")
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	queued := "unknown"
	if value, ok := result["queued"].(bool); ok {
		queued = fmt.Sprint(value)
	}
	cli.PrintTable(os.Stdout, []string{"MESSAGE_ID", "RUN_ID", "QUEUED", "CREATED"}, [][]string{{strVal(result, "message_id"), strVal(result, "task_id"), queued, strVal(result, "created_at")}})
	return nil
}
