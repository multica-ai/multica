package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"

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

func init() {
	for _, c := range []*cobra.Command{chatHistoryCmd, chatThreadCmd} {
		c.Flags().Int("limit", 0, "Maximum number of messages to return (the server clamps the range)")
		c.Flags().String("before", "", "Opaque cursor (a next_cursor from a prior page) to read older messages")
		c.Flags().String("output", "json", "Output format: table or json")
	}
	chatCmd.AddCommand(chatHistoryCmd)
	chatCmd.AddCommand(chatThreadCmd)
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

var chatReplyCmd = &cobra.Command{
	Use:   "reply",
	Short: "Write the Secretary's reply into a chat session (Secretary only)",
	Long: `Write an assistant message into a chat session from a run that is not a chat turn,
for example when a later run has the answer to a question the boss asked in chat.

The run must be the Secretary's, live, and linked to the session (its chat_session_id
names the session), and the run's originator must own the session. An archived
session refuses the reply: report through the inbox instead. A session takes at most
10 replies a minute.

The reply is Markdown, read from --file (a path inside the working directory) or,
when --file is omitted, from stdin.`,
	Args: cobra.NoArgs,
	RunE: runChatReply,
}

func init() {
	chatReplyCmd.Flags().String("session", "", "Chat session id to reply in (required)")
	chatReplyCmd.Flags().String("file", "", "Markdown file with the reply (default: read stdin)")
	chatCmd.AddCommand(chatReplyCmd)
}

func runChatReply(cmd *cobra.Command, _ []string) error {
	session, _ := cmd.Flags().GetString("session")
	if strings.TrimSpace(session) == "" {
		return fmt.Errorf("--session is required")
	}
	filePath, _ := cmd.Flags().GetString("file")

	var data []byte
	if filePath != "" {
		if err := ensureFileFlagWithinWorkdir(cmd, "file", "content", filePath); err != nil {
			return err
		}
		read, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read file for --file: %w", err)
		}
		data = read
	} else {
		read, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		data = read
	}
	content := strings.TrimSuffix(string(data), "\n")
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("reply content is empty: pass --file or pipe the reply on stdin")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp map[string]any
	path := "/api/chat/sessions/" + url.PathEscape(strings.TrimSpace(session)) + "/reply"
	if err := client.PostJSON(ctx, path, map[string]string{"content": content}, &resp); err != nil {
		return fmt.Errorf("reply in chat: %w", err)
	}
	return cli.PrintJSON(os.Stdout, resp)
}
