// CEREBRO-PATCH(chat-cli): TECH-3183 — agent chat reply with attachment via CLI.
// CEREBRO-PATCH(fir-125-channel-cli): multica chat session list — workspace-level listing.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Interact with chat sessions (cerebro)",
}

var chatSessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage chat sessions",
}

var chatSessionSendCmd = &cobra.Command{
	Use:   "send <session-id>",
	Short: "Send an assistant message to a chat session",
	Long:  "Post an assistant message to a chat session where this agent is the assigned agent. Optionally attach a file.",
	Example: `  # Send a plain message
  $ multica chat session send 13dec5c7-... --content "Here is my report."

  # Send a message with a file attachment
  $ multica chat session send 13dec5c7-... --content "Here is the file." --attachment report.md`,
	Args: exactArgs(1),
	RunE: runChatSessionSend,
}

var chatSessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List chat sessions",
	RunE:  runChatSessionList,
}

func init() {
	chatCmd.AddCommand(chatSessionCmd)
	chatSessionCmd.AddCommand(chatSessionSendCmd)
	chatSessionCmd.AddCommand(chatSessionListCmd)

	chatSessionSendCmd.Flags().String("content", "", "Message text to send (required)")
	chatSessionSendCmd.Flags().StringArray("attachment", nil, "Local file to attach (may be repeated)")
	_ = chatSessionSendCmd.MarkFlagRequired("content")

	chatSessionListCmd.Flags().String("output", "table", "Output format: table or json")
	chatSessionListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")
	chatSessionListCmd.Flags().String("status", "", "Filter by status: all, archived (default: active only). Ignored when --all is set.")
	chatSessionListCmd.Flags().Bool("all", false, "List all chat sessions in workspace across all creators")
}

func runChatSessionSend(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	sessionID := args[0]

	content, _ := cmd.Flags().GetString("content")
	attachments, _ := cmd.Flags().GetStringArray("attachment")

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// Create the assistant message.
	var resp struct {
		MessageID string `json:"message_id"`
		CreatedAt string `json:"created_at"`
	}
	if err := client.PostJSON(ctx, "/api/chat/sessions/"+sessionID+"/agent-message", map[string]any{
		"content": content,
	}, &resp); err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Message created: %s\n", resp.MessageID)

	// Upload attachments and link them to the message.
	for _, filePath := range attachments {
		if !filepath.IsAbs(filePath) {
			if abs, err := filepath.Abs(filePath); err == nil {
				filePath = abs
			}
		}
		info, err := os.Stat(filePath)
		if err != nil {
			return fmt.Errorf("cannot read file %s: %w", filePath, err)
		}
		if info.IsDir() {
			return fmt.Errorf("%s is a directory, not a file", filePath)
		}
		const maxSize = 100 << 20
		if info.Size() > maxSize {
			return fmt.Errorf("file %s is %d bytes, max is %d (100 MB)", filePath, info.Size(), maxSize)
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read file %s: %w", filePath, err)
		}
		result, err := client.UploadAttachmentTo(ctx, data, filepath.Base(filePath), cli.AttachmentTarget{
			ChatMessageID: resp.MessageID,
		})
		if err != nil {
			return fmt.Errorf("attach %s: %w", filepath.Base(filePath), err)
		}
		fmt.Fprintf(os.Stderr, "Attached: %s\n", strVal(result, "filename"))
	}

	return cli.PrintJSON(os.Stdout, map[string]any{
		"message_id": resp.MessageID,
		"created_at": resp.CreatedAt,
	})
}

func runChatSessionList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	path := "/api/chat/sessions"
	if all, _ := cmd.Flags().GetBool("all"); all {
		path += "?all=true"
	} else if status, _ := cmd.Flags().GetString("status"); status != "" {
		path += "?status=" + status
	}

	var result []any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list chat sessions: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "TITLE", "CREATOR", "AGENT", "STATUS", "UPDATED"}
	rows := make([][]string, 0, len(result))
	for _, raw := range result {
		s, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		updated := strVal(s, "updated_at")
		if len(updated) >= 10 {
			updated = updated[:10]
		}
		unread := ""
		if u, ok := s["has_unread"].(bool); ok && u {
			unread = " *"
		}
		rows = append(rows, []string{
			displayID(strVal(s, "id"), fullID),
			strVal(s, "title") + unread,
			displayID(strVal(s, "creator_id"), fullID),
			displayID(strVal(s, "agent_id"), fullID),
			strVal(s, "status"),
			updated,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}
