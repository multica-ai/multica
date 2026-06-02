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

// ---------------------------------------------------------------------------
// Channel commands — lightweight human+agent collaboration (OPE-1943).
// V2: channels contain flat messages; threads are created implicitly on reply.
// ---------------------------------------------------------------------------

var channelCmd = &cobra.Command{
	Use:   "channel",
	Short: "Work with channels (human+agent discussion)",
}

var channelListCmd = &cobra.Command{
	Use:   "list",
	Short: "List channels visible to you",
	RunE:  runChannelList,
}

var channelCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new channel",
	RunE:  runChannelCreate,
}

// ---- message subcommands ----

var channelMessageCmd = &cobra.Command{
	Use:   "message",
	Short: "Work with channel messages",
}

var channelMessageListCmd = &cobra.Command{
	Use:   "list <channel-id>",
	Short: "List top-level messages in a channel",
	Args:  exactArgs(1),
	RunE:  runChannelMessageList,
}

var channelMessageSendCmd = &cobra.Command{
	Use:   "send <channel-id>",
	Short: "Post a message into a channel",
	Args:  exactArgs(1),
	RunE:  runChannelMessageSend,
}

var channelMessageReplyCmd = &cobra.Command{
	Use:   "reply <channel-id> <message-id>",
	Short: "Reply to a message (creates thread if needed)",
	Args:  exactArgs(2),
	RunE:  runChannelMessageReply,
}

// ---- member subcommands ----

var channelMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Manage channel members",
}

var channelMemberListCmd = &cobra.Command{
	Use:   "list <channel-id>",
	Short: "List members of a channel",
	Args:  exactArgs(1),
	RunE:  runChannelMemberList,
}

var channelMemberAddCmd = &cobra.Command{
	Use:   "add <channel-id>",
	Short: "Add a member to a channel",
	Args:  exactArgs(1),
	RunE:  runChannelMemberAdd,
}

var channelMemberRemoveCmd = &cobra.Command{
	Use:   "remove <channel-id>",
	Short: "Remove a member from a channel",
	Args:  exactArgs(1),
	RunE:  runChannelMemberRemove,
}

// ---- thread subcommands (V1 backward compat) ----

var channelThreadCmd = &cobra.Command{
	Use:   "thread",
	Short: "Work with channel threads (legacy)",
}

var channelThreadListCmd = &cobra.Command{
	Use:   "list <channel-id>",
	Short: "List threads in a channel",
	Args:  exactArgs(1),
	RunE:  runChannelThreadList,
}

var channelThreadCreateCmd = &cobra.Command{
	Use:   "create <channel-id>",
	Short: "Open a new thread (optionally with an opening message)",
	Args:  exactArgs(1),
	RunE:  runChannelThreadCreate,
}

// ---- context subcommand ----

var channelContextCmd = &cobra.Command{
	Use:   "context <channel-id>",
	Short: "Get channel context (info, members, recent messages) for agents",
	Args:  exactArgs(1),
	RunE:  runChannelContext,
}

func init() {
	channelCmd.AddCommand(channelListCmd)
	channelCmd.AddCommand(channelCreateCmd)
	channelCmd.AddCommand(channelMessageCmd)
	channelCmd.AddCommand(channelMemberCmd)
	channelCmd.AddCommand(channelThreadCmd)
	channelCmd.AddCommand(channelContextCmd)

	channelMessageCmd.AddCommand(channelMessageListCmd)
	channelMessageCmd.AddCommand(channelMessageSendCmd)
	channelMessageCmd.AddCommand(channelMessageReplyCmd)

	channelMemberCmd.AddCommand(channelMemberListCmd)
	channelMemberCmd.AddCommand(channelMemberAddCmd)
	channelMemberCmd.AddCommand(channelMemberRemoveCmd)

	channelThreadCmd.AddCommand(channelThreadListCmd)
	channelThreadCmd.AddCommand(channelThreadCreateCmd)

	channelListCmd.Flags().String("output", "table", "Output format: table or json")
	channelListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	channelCreateCmd.Flags().String("name", "", "Channel name (required)")
	channelCreateCmd.Flags().String("description", "", "Channel description")
	channelCreateCmd.Flags().String("access-mode", "open", "Access mode: open or invite")
	channelCreateCmd.Flags().String("output", "json", "Output format: table or json")

	channelMessageListCmd.Flags().String("output", "table", "Output format: table or json")
	channelMessageListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	channelMessageSendCmd.Flags().String("content", "", "Message content (required)")
	channelMessageSendCmd.Flags().String("output", "json", "Output format: table or json")

	channelMessageReplyCmd.Flags().String("content", "", "Reply content (required)")
	channelMessageReplyCmd.Flags().String("output", "json", "Output format: table or json")

	channelMemberListCmd.Flags().String("output", "table", "Output format: table or json")
	channelMemberListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	channelMemberAddCmd.Flags().String("user-id", "", "User ID to add (required)")
	channelMemberAddCmd.Flags().String("role", "member", "Role: owner or member")
	channelMemberAddCmd.Flags().String("output", "json", "Output format")

	channelMemberRemoveCmd.Flags().String("user-id", "", "User ID to remove (required)")
	channelMemberRemoveCmd.Flags().String("output", "json", "Output format")

	channelThreadListCmd.Flags().String("output", "table", "Output format: table or json")
	channelThreadListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	channelThreadCreateCmd.Flags().String("title", "", "Thread title")
	channelThreadCreateCmd.Flags().String("content", "", "Opening message content")
	channelThreadCreateCmd.Flags().String("output", "json", "Output format: table or json")

	channelContextCmd.Flags().Int("recent", 20, "Number of recent messages to include")
	channelContextCmd.Flags().String("output", "json", "Output format")
}

func runChannelList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	path := "/api/channels"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}
	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list channels: %w", err)
	}
	channelsRaw, _ := result["channels"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, channelsRaw)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "NAME", "ACCESS", "MEMBER", "UNREAD"}
	rows := make([][]string, 0, len(channelsRaw))
	for _, raw := range channelsRaw {
		c, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, []string{
			displayID(strVal(c, "id"), fullID),
			strVal(c, "name"),
			strVal(c, "access_mode"),
			boolMark(c, "is_member"),
			boolMark(c, "has_unread"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runChannelCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}
	access, _ := cmd.Flags().GetString("access-mode")
	desc, _ := cmd.Flags().GetString("description")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"name": name, "access_mode": access}
	if desc != "" {
		body["description"] = desc
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/channels", body, &result); err != nil {
		return fmt.Errorf("create channel: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---- message commands (V2) ----

func runChannelMessageList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/channels/" + args[0] + "/messages"
	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list messages: %w", err)
	}
	messagesRaw, _ := result["messages"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "AUTHOR", "CONTENT", "REPLIES", "CREATED"}
	rows := make([][]string, 0, len(messagesRaw))
	for _, raw := range messagesRaw {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		created := strVal(m, "created_at")
		if len(created) >= 19 {
			created = created[:19]
		}
		content := strVal(m, "content")
		if len(content) > 60 {
			content = content[:57] + "..."
		}
		rows = append(rows, []string{
			displayID(strVal(m, "id"), fullID),
			strVal(m, "author_type"),
			content,
			intStr(m, "reply_count"),
			created,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runChannelMessageSend(cmd *cobra.Command, args []string) error {
	content, _ := cmd.Flags().GetString("content")
	if content == "" {
		return fmt.Errorf("--content is required")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/channels/" + args[0] + "/messages"
	body := map[string]any{"content": content}
	var result map[string]any
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("send message: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runChannelMessageReply(cmd *cobra.Command, args []string) error {
	content, _ := cmd.Flags().GetString("content")
	if content == "" {
		return fmt.Errorf("--content is required")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/channels/" + args[0] + "/messages/" + args[1] + "/reply"
	body := map[string]any{"content": content}
	var result map[string]any
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("reply: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---- member commands ----

func runChannelMemberList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/channels/" + args[0] + "/members"
	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list members: %w", err)
	}
	membersRaw, _ := result["members"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"USER_ID", "NAME", "ROLE", "JOINED"}
	rows := make([][]string, 0, len(membersRaw))
	for _, raw := range membersRaw {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		joined := strVal(m, "joined_at")
		if len(joined) >= 10 {
			joined = joined[:10]
		}
		rows = append(rows, []string{
			displayID(strVal(m, "user_id"), fullID),
			strVal(m, "user_name"),
			strVal(m, "role"),
			joined,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runChannelMemberAdd(cmd *cobra.Command, args []string) error {
	userID, _ := cmd.Flags().GetString("user-id")
	if userID == "" {
		return fmt.Errorf("--user-id is required")
	}
	role, _ := cmd.Flags().GetString("role")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/channels/" + args[0] + "/members"
	body := map[string]any{"user_id": userID, "role": role}
	var result map[string]any
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runChannelMemberRemove(cmd *cobra.Command, args []string) error {
	userID, _ := cmd.Flags().GetString("user-id")
	if userID == "" {
		return fmt.Errorf("--user-id is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/channels/" + args[0] + "/members/" + userID
	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Removed user %s from channel %s\n", userID, args[0])
	return nil
}

// ---- thread commands (V1 backward compat) ----

func runChannelThreadList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/channels/"+args[0]+"/threads", &result); err != nil {
		return fmt.Errorf("list threads: %w", err)
	}
	threadsRaw, _ := result["threads"].([]any)

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, threadsRaw)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "TITLE", "MESSAGES", "ISSUES", "LAST ACTIVITY"}
	rows := make([][]string, 0, len(threadsRaw))
	for _, raw := range threadsRaw {
		t, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		last := strVal(t, "last_message_at")
		if len(last) >= 19 {
			last = last[:19]
		}
		rows = append(rows, []string{
			displayID(strVal(t, "id"), fullID),
			strVal(t, "title"),
			intStr(t, "message_count"),
			intStr(t, "issue_count"),
			last,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runChannelThreadCreate(cmd *cobra.Command, args []string) error {
	title, _ := cmd.Flags().GetString("title")
	content, _ := cmd.Flags().GetString("content")
	if title == "" && content == "" {
		return fmt.Errorf("provide --title and/or --content")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{}
	if title != "" {
		body["title"] = title
	}
	if content != "" {
		body["content"] = content
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/channels/"+args[0]+"/threads", body, &result); err != nil {
		return fmt.Errorf("create thread: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ---- context command ----

func runChannelContext(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	recent, _ := cmd.Flags().GetInt("recent")
	path := fmt.Sprintf("/api/channels/%s/context?recent=%d", args[0], recent)
	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("get context: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

// boolMark renders a checkmark for a truthy bool field, blank otherwise.
func boolMark(m map[string]any, key string) string {
	if v, ok := m[key].(bool); ok && v {
		return "✓"
	}
	return ""
}

// intStr renders a numeric JSON field as an integer string.
func intStr(m map[string]any, key string) string {
	return fmt.Sprintf("%d", int64(numVal(m, key)))
}
