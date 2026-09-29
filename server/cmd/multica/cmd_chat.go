package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Work with the current chat conversation",
}

var chatHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Overview of the channel this conversation is in (messages + thread list)",
	Long: `Show the overview of the chat channel (e.g. Slack) this conversation is in: the
recent top-level messages, and for each thread its thread_id, reply_count, and
latest_reply. It does NOT expand thread contents — it is the table of contents.

To read a specific thread's messages, take a thread_id from here and run
"multica chat thread <thread_id>".

It is the SAME command regardless of which channel the conversation came from,
and it reads only the conversation you are currently running for — it cannot
read any other session or channel.`,
	Args: cobra.NoArgs,
	RunE: runChatHistory,
}

var chatThreadCmd = &cobra.Command{
	Use:   "thread [id]",
	Short: "Read one thread's messages (the current thread, or a specific id)",
	Long: `Read the messages of a single thread.

With no id, read the thread you are currently in (the one you were @mentioned in).
With an id — a thread_id from "multica chat history" — read that specific thread.
Either way the thread is within the channel you are in; you cannot read another
channel.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runChatThread,
}

var chatSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List all chat sessions you can access",
	Args:  cobra.NoArgs,
	RunE:  runChatSessions,
}

var chatMessagesCmd = &cobra.Command{
	Use:   "messages <session-id>",
	Short: "Read a chat session's messages",
	Args:  cobra.ExactArgs(1),
	RunE:  runChatMessages,
}

func init() {
	for _, c := range []*cobra.Command{chatHistoryCmd, chatThreadCmd} {
		c.Flags().Int("limit", 0, "Maximum number of messages to return (the server clamps the range)")
		c.Flags().String("before", "", "Opaque cursor (a next_cursor from a prior page) to read older messages")
		c.Flags().String("output", "json", "Output format: table or json")
	}
	chatSessionsCmd.Flags().String("status", "active", "Session status: active, archived, or all")
	chatSessionsCmd.Flags().String("output", "json", "Output format: json")
	chatMessagesCmd.Flags().Int("limit", 50, "Number of messages to return (1-100)")
	chatMessagesCmd.Flags().String("before-created-at", "", "RFC3339Nano cursor timestamp from next_cursor")
	chatMessagesCmd.Flags().String("before-id", "", "UUID cursor ID from next_cursor")
	chatMessagesCmd.Flags().String("output", "json", "Output format: json")
	chatCmd.AddCommand(chatHistoryCmd)
	chatCmd.AddCommand(chatThreadCmd)
	chatCmd.AddCommand(chatSessionsCmd)
	chatCmd.AddCommand(chatMessagesCmd)
}

func runChatSessions(cmd *cobra.Command, _ []string) error {
	if err := requireChatJSONOutput(cmd); err != nil {
		return err
	}
	status, _ := cmd.Flags().GetString("status")
	if status != "active" && status != "archived" && status != "all" {
		return fmt.Errorf("invalid chat session status %q: want active, archived, or all", status)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	// The endpoint exposes active sessions by default and both statuses with
	// status=all. Fetch all for the archived view, then filter locally so the
	// command has stable active|archived|all semantics without changing the API.
	apiStatus := status
	if status == "archived" {
		apiStatus = "all"
	}
	path := "/api/chat/sessions?" + url.Values{"status": {apiStatus}}.Encode()
	var resp []map[string]any
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("list chat sessions: %w", err)
	}
	if resp == nil {
		return fmt.Errorf("list chat sessions: invalid response")
	}

	sessions := make([]map[string]any, 0, len(resp))
	for _, session := range resp {
		for _, key := range []string{"id", "status", "updated_at", "created_at", "agent_id", "creator_id"} {
			if value, ok := session[key].(string); !ok || value == "" {
				return fmt.Errorf("list chat sessions: invalid response")
			}
		}
		if status != "all" && session["status"] != status {
			continue
		}
		sessions = append(sessions, session)
	}
	return cli.PrintJSON(os.Stdout, sessions)
}

type chatMessagesCursor struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

type chatMessagesPage struct {
	Messages   []json.RawMessage   `json:"messages"`
	Limit      int                 `json:"limit"`
	HasMore    bool                `json:"has_more"`
	NextCursor *chatMessagesCursor `json:"next_cursor"`
}

func runChatMessages(cmd *cobra.Command, args []string) error {
	if err := requireChatJSONOutput(cmd); err != nil {
		return err
	}
	sessionID := args[0]
	if _, err := uuid.Parse(sessionID); err != nil {
		return fmt.Errorf("invalid chat session ID %q: expected UUID", sessionID)
	}
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 1 || limit > 100 {
		return fmt.Errorf("invalid chat message limit %d: want 1-100", limit)
	}
	beforeCreatedAt, _ := cmd.Flags().GetString("before-created-at")
	beforeID, _ := cmd.Flags().GetString("before-id")
	if (beforeCreatedAt == "") != (beforeID == "") {
		return fmt.Errorf("--before-created-at and --before-id must be provided together")
	}
	if beforeCreatedAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, beforeCreatedAt); err != nil {
			return fmt.Errorf("invalid --before-created-at %q: expected RFC3339Nano", beforeCreatedAt)
		}
		if _, err := uuid.Parse(beforeID); err != nil {
			return fmt.Errorf("invalid --before-id %q: expected UUID", beforeID)
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	params := url.Values{"limit": {strconv.Itoa(limit)}}
	if beforeCreatedAt != "" {
		params.Set("before_created_at", beforeCreatedAt)
		params.Set("before_id", beforeID)
	}
	path := "/api/chat/sessions/" + url.PathEscape(sessionID) + "/messages/page?" + params.Encode()
	var page chatMessagesPage
	if err := client.GetJSON(ctx, path, &page); err != nil {
		return fmt.Errorf("read chat messages: %w", err)
	}
	if page.Messages == nil || page.Limit < 1 || page.Limit > 100 {
		return fmt.Errorf("read chat messages: invalid response")
	}
	if page.HasMore {
		if page.NextCursor == nil || page.NextCursor.CreatedAt == "" || page.NextCursor.ID == "" {
			return fmt.Errorf("read chat messages: invalid response")
		}
		if _, err := time.Parse(time.RFC3339Nano, page.NextCursor.CreatedAt); err != nil {
			return fmt.Errorf("read chat messages: invalid response")
		}
		if _, err := uuid.Parse(page.NextCursor.ID); err != nil {
			return fmt.Errorf("read chat messages: invalid response")
		}
	}
	if !page.HasMore && page.NextCursor != nil {
		return fmt.Errorf("read chat messages: invalid response")
	}
	return cli.PrintJSON(os.Stdout, page)
}

func requireChatJSONOutput(cmd *cobra.Command) error {
	output, _ := cmd.Flags().GetString("output")
	if output != "json" {
		return fmt.Errorf("chat sessions and messages only support --output json")
	}
	return nil
}

func runChatHistory(cmd *cobra.Command, _ []string) error {
	resp, err := fetchChatRead(cmd, "/api/chat/history", "")
	if err != nil {
		return err
	}
	return renderChatRead(cmd, resp, true)
}

func runChatThread(cmd *cobra.Command, args []string) error {
	threadID := ""
	if len(args) == 1 {
		threadID = args[0]
	}
	resp, err := fetchChatRead(cmd, "/api/chat/thread", threadID)
	if err != nil {
		return err
	}
	return renderChatRead(cmd, resp, false)
}

// fetchChatRead builds the request (shared --limit/--before paging, plus the
// optional thread id) and decodes the response.
func fetchChatRead(cmd *cobra.Command, basePath, threadID string) (map[string]any, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	limit, _ := cmd.Flags().GetInt("limit")
	before, _ := cmd.Flags().GetString("before")

	q := url.Values{}
	if threadID != "" {
		q.Set("id", threadID)
	}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if before != "" {
		q.Set("before", before)
	}
	path := basePath
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	var resp map[string]any
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("read chat: %w", err)
	}
	return resp, nil
}

// renderChatRead prints the response as JSON (default) or a table. The overview
// table adds the thread columns so the agent can pick a thread_id to drill into.
func renderChatRead(cmd *cobra.Command, resp map[string]any, overview bool) error {
	output, _ := cmd.Flags().GetString("output")
	if output != "table" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	if note := strVal(resp, "note"); note != "" {
		fmt.Fprintln(os.Stdout, note)
		return nil
	}
	msgs, _ := resp["messages"].([]any)
	var headers []string
	if overview {
		headers = []string{"TS", "ROLE", "AUTHOR", "THREAD_ID", "REPLIES", "TEXT"}
	} else {
		headers = []string{"TS", "ROLE", "AUTHOR", "TEXT"}
	}
	rows := make([][]string, 0, len(msgs))
	for _, mi := range msgs {
		m, ok := mi.(map[string]any)
		if !ok {
			continue
		}
		if overview {
			rows = append(rows, []string{strVal(m, "ts"), strVal(m, "role"), strVal(m, "author"), strVal(m, "thread_id"), numVal(m, "reply_count"), strVal(m, "text")})
		} else {
			rows = append(rows, []string{strVal(m, "ts"), strVal(m, "role"), strVal(m, "author"), strVal(m, "text")})
		}
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// numVal renders a numeric JSON field as a string, blank when zero/absent.
func numVal(m map[string]any, key string) string {
	if v, ok := m[key].(float64); ok && v != 0 {
		return strconv.Itoa(int(v))
	}
	return ""
}
