package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/util"
)

// ---------------------------------------------------------------------------
// channel command tree
// ---------------------------------------------------------------------------

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Work with channels",
}

var channelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List channels in the workspace",
	RunE:  runChannelList,
}

var channelMessagesCmd = &cobra.Command{
	Use:   "messages <channel-id>",
	Short: "List recent messages in a channel",
	Args:  exactArgs(1),
	RunE:  runChannelMessages,
}

var channelSendCmd = &cobra.Command{
	Use:   "send <channel-id>",
	Short: "Send a message to a channel",
	Args:  exactArgs(1),
	RunE:  runChannelSend,
}

var channelMembersCmd = &cobra.Command{
	Use:   "members <channel-id>",
	Short: "List members of a channel",
	Args:  exactArgs(1),
	RunE:  runChannelMembers,
}

func init() {
	channelCmd.AddCommand(channelListCmd)
	channelCmd.AddCommand(channelMessagesCmd)
	channelCmd.AddCommand(channelSendCmd)
	channelCmd.AddCommand(channelMembersCmd)

	channelListCmd.Flags().String("output", "table", "Output format: table or json")

	channelMessagesCmd.Flags().Int("limit", 20, "Maximum number of messages to return")
	channelMessagesCmd.Flags().String("output", "json", "Output format: table or json")

	channelSendCmd.Flags().String("content", "", "Message content (decodes \\n, \\r, \\t, \\\\)")
	channelSendCmd.Flags().Bool("content-stdin", false, "Read message content from stdin")
	channelSendCmd.Flags().String("output", "json", "Output format: table or json")

	channelMembersCmd.Flags().String("output", "table", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// channel list
// ---------------------------------------------------------------------------

func runChannelList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	wsID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}
	client.WorkspaceID = wsID

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var channels []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		Description string `json:"description"`
		IsMember    bool   `json:"is_member"`
	}
	if err := client.GetJSON(ctx, "/api/channels", &channels); err != nil {
		return fmt.Errorf("list channels: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(channels)
	}

	if len(channels) == 0 {
		fmt.Fprintln(os.Stderr, "No channels found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tTYPE\tMEMBER")
	for _, ch := range channels {
		member := "no"
		if ch.IsMember {
			member = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ch.ID, ch.Name, ch.Type, member)
	}
	return w.Flush()
}

// ---------------------------------------------------------------------------
// channel messages
// ---------------------------------------------------------------------------

func runChannelMessages(cmd *cobra.Command, args []string) error {
	channelID := args[0]
	if _, err := util.ParseUUID(channelID); err != nil {
		return fmt.Errorf("invalid channel-id: %w", err)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	wsID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}
	client.WorkspaceID = wsID

	limit, _ := cmd.Flags().GetInt("limit")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var messages []struct {
		ID         string `json:"id"`
		AuthorType string `json:"author_type"`
		AuthorID   string `json:"author_id"`
		Content    string `json:"content"`
		Status     string `json:"status"`
		CreatedAt  string `json:"created_at"`
	}
	path := fmt.Sprintf("/api/channels/%s/messages?limit=%d", channelID, limit)
	if err := client.GetJSON(ctx, path, &messages); err != nil {
		return fmt.Errorf("list channel messages: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(messages)
	}

	if len(messages) == 0 {
		fmt.Fprintln(os.Stderr, "No messages found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tAUTHOR_TYPE\tCONTENT")
	for _, m := range messages {
		ts := m.CreatedAt
		if t, err := time.Parse(time.RFC3339, m.CreatedAt); err == nil {
			ts = t.Local().Format("15:04")
		}
		content := m.Content
		if len(content) > 80 {
			content = content[:77] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", ts, m.AuthorType, content)
	}
	return w.Flush()
}

// ---------------------------------------------------------------------------
// channel send
// ---------------------------------------------------------------------------

func runChannelSend(cmd *cobra.Command, args []string) error {
	channelID := args[0]
	if _, err := util.ParseUUID(channelID); err != nil {
		return fmt.Errorf("invalid channel-id: %w", err)
	}

	content, hasContent, err := resolveTextFlag(cmd, "content")
	if err != nil {
		return err
	}
	if !hasContent || content == "" {
		return fmt.Errorf("--content is required (or pipe via --content-stdin)")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	wsID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}
	client.WorkspaceID = wsID

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"content": content}
	var result map[string]any
	if err := client.PostJSON(ctx, fmt.Sprintf("/api/channels/%s/messages", channelID), body, &result); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	id, _ := result["id"].(string)
	fmt.Printf("Message sent: %s\n", id)
	return nil
}

// ---------------------------------------------------------------------------
// channel members
// ---------------------------------------------------------------------------

func runChannelMembers(cmd *cobra.Command, args []string) error {
	channelID := args[0]
	if _, err := util.ParseUUID(channelID); err != nil {
		return fmt.Errorf("invalid channel-id: %w", err)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	wsID, err := requireWorkspaceID(cmd)
	if err != nil {
		return err
	}
	client.WorkspaceID = wsID

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var members []struct {
		ID         string `json:"id"`
		MemberType string `json:"member_type"`
		MemberID   string `json:"member_id"`
		Name       string `json:"name"`
		Role       string `json:"role"`
		JoinedAt   string `json:"joined_at"`
	}
	if err := client.GetJSON(ctx, fmt.Sprintf("/api/channels/%s/members", channelID), &members); err != nil {
		return fmt.Errorf("list channel members: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(members)
	}

	if len(members) == 0 {
		fmt.Fprintln(os.Stderr, "No members found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tTYPE\tROLE\tMEMBER_ID")
	for _, m := range members {
		name := m.Name
		if name == "" {
			name = m.MemberID
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, m.MemberType, m.Role, m.MemberID)
	}
	return w.Flush()
}
