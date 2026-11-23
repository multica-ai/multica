package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var chatListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your chat sessions",
	Long: `List the chat sessions you started in this workspace, the same ones the
Chat page lists. Sessions with an agent you can no longer access are left out.

By default only active sessions are listed. Use --status archived or
--status all to include archived ones.`,
	Args: cobra.NoArgs,
	RunE: runChatList,
}

var chatArchiveCmd = &cobra.Command{
	Use:   "archive <session-id>...",
	Short: "Archive one or more chat sessions",
	Long: `Archive chat sessions by id, as archiving them on the Chat page does. Work
still queued for a session that came from a chat channel is cancelled.

Each id is reported on its own line. The command exits non-zero if any of them
fails. Archiving a session that is already archived succeeds.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error { return runChatSetArchived(cmd, args, true) },
}

var chatRestoreCmd = &cobra.Command{
	Use:     "restore <session-id>...",
	Aliases: []string{"unarchive"},
	Short:   "Restore one or more archived chat sessions",
	Long: `Move archived chat sessions back to the active list.

Each id is reported on its own line. The command exits non-zero if any of them
fails. Restoring a session that is already active succeeds.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error { return runChatSetArchived(cmd, args, false) },
}

var chatSessionStatuses = []string{"active", "archived", "all"}

func init() {
	chatListCmd.Flags().String("status", "active", "Which sessions to list: active, archived or all")
	chatListCmd.Flags().Int("limit", 0, "Maximum number of sessions to print (0 for all)")
	chatListCmd.Flags().String("output", "table", "Output format: table or json")
	for _, c := range []*cobra.Command{chatArchiveCmd, chatRestoreCmd} {
		c.Flags().Bool("dry-run", false, "Print the sessions that would change without changing them")
		c.Flags().String("output", "table", "Output format: table or json")
	}
	chatCmd.AddCommand(chatListCmd)
	chatCmd.AddCommand(chatArchiveCmd)
	chatCmd.AddCommand(chatRestoreCmd)
}

func runChatList(cmd *cobra.Command, _ []string) error {
	status, _ := cmd.Flags().GetString("status")
	if !slices.Contains(chatSessionStatuses, status) {
		return fmt.Errorf("--status must be one of %s", strings.Join(chatSessionStatuses, ", "))
	}
	limit, _ := cmd.Flags().GetInt("limit")
	if limit < 0 {
		return errors.New("--limit must not be negative")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	// The server lists active sessions, or every session with status=all.
	// Archived-only is the "all" list without the active ones.
	path := "/api/chat/sessions"
	if status != "active" {
		path += "?" + url.Values{"status": {"all"}}.Encode()
	}
	var sessions []map[string]any
	if err := client.GetJSON(ctx, path, &sessions); err != nil {
		return fmt.Errorf("list chat sessions: %w", err)
	}
	if status == "archived" {
		archived := sessions[:0]
		for _, s := range sessions {
			if strVal(s, "status") == "archived" {
				archived = append(archived, s)
			}
		}
		sessions = archived
	}
	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, sessions)
	}
	rows := make([][]string, 0, len(sessions))
	for _, s := range sessions {
		rows = append(rows, []string{
			strVal(s, "id"),
			strVal(s, "title"),
			strVal(s, "status"),
			numVal(s, "unread_count"),
			chatSessionLastActivity(s),
		})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "TITLE", "STATUS", "UNREAD", "LAST_ACTIVITY"}, rows)
	return nil
}

// chatSessionLastActivity is when the session last had a message, which is how
// the Chat page orders and dates its rows; a session with none falls back to
// its last update.
func chatSessionLastActivity(s map[string]any) string {
	if last, ok := s["last_message"].(map[string]any); ok {
		if at := strVal(last, "created_at"); at != "" {
			return at
		}
	}
	return strVal(s, "updated_at")
}

type chatArchiveResult struct {
	ID     string `json:"id"`
	Status string `json:"status,omitempty"`
	Title  string `json:"title,omitempty"`
	DryRun bool   `json:"dry_run,omitempty"`
	Error  string `json:"error,omitempty"`
}

func runChatSetArchived(cmd *cobra.Command, ids []string, archived bool) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	output, _ := cmd.Flags().GetString("output")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	want := "active"
	verb := "restore"
	if archived {
		want = "archived"
		verb = "archive"
	}

	results := make([]chatArchiveResult, 0, len(ids))
	failed := 0
	for _, id := range uniqueStrings(ids) {
		result := chatArchiveResult{ID: id, DryRun: dryRun}
		if dryRun {
			result.Status = want
		} else {
			var session map[string]any
			ctx, cancel := cli.APIContext(context.Background())
			err := client.PatchJSON(ctx, "/api/chat/sessions/"+url.PathEscape(id)+"/archive", map[string]bool{"archived": archived}, &session)
			cancel()
			if err != nil {
				result.Error = err.Error()
				failed++
			} else {
				result.Status = strVal(session, "status")
				result.Title = strVal(session, "title")
			}
		}
		results = append(results, result)
	}

	if output == "json" {
		if err := cli.PrintJSON(os.Stdout, results); err != nil {
			return err
		}
	} else {
		for _, r := range results {
			switch {
			case r.Error != "":
				fmt.Fprintf(os.Stderr, "%s: failed to %s: %s\n", r.ID, verb, r.Error)
			case r.DryRun:
				fmt.Fprintf(os.Stdout, "%s: would %s\n", r.ID, verb)
			default:
				fmt.Fprintf(os.Stdout, "%s: %s  %s\n", r.ID, r.Status, r.Title)
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d chat sessions failed to %s", failed, len(results), verb)
	}
	return nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
