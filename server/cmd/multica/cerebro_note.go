// CEREBRO-PATCH(cerebro-note-cli): FIR-2022 cerebro-only file — read + search
// notes/documents and their comments from the CLI. The server surface lives in
// server/internal/cerebro/note (GET /api/notes/{id}, /{id}/comments, /search).
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// noteCmd is the `multica note ...` command tree. A "note" here is any document
// (artifact) the caller may see — every kind, not only personal notes — so an
// agent can read a note's body, read its comments, and search across title,
// body and comments. Access is the caller's: an agent token resolves to its
// owner, so private notes never leak.
var noteCmd = &cobra.Command{
	Use:   "note",
	Short: "Read and search notes/documents and their comments (cerebro)",
}

var noteReadCmd = &cobra.Command{
	Use:   "read <id>",
	Short: "Read a note/document (title + body + meta)",
	Args:  exactArgs(1),
	RunE:  runNoteRead,
}

var noteCommentsCmd = &cobra.Command{
	Use:   "comments <id>",
	Short: "List the comments on a note/document",
	Args:  exactArgs(1),
	RunE:  runNoteComments,
}

var noteSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search notes/documents by title, body and comments",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runNoteSearch,
}

func init() {
	noteCmd.AddCommand(noteReadCmd)
	noteCmd.AddCommand(noteCommentsCmd)
	noteCmd.AddCommand(noteSearchCmd)

	noteReadCmd.Flags().String("output", "json", "Output format: table or json")
	noteCommentsCmd.Flags().String("output", "json", "Output format: table or json")

	noteSearchCmd.Flags().String("kind", "", "Restrict to a kind: report|plan|decision|diagram|note")
	noteSearchCmd.Flags().Int("limit", 20, "Maximum number of results (max 50)")
	noteSearchCmd.Flags().Int("offset", 0, "Pagination offset")
	noteSearchCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// read
// ---------------------------------------------------------------------------

func runNoteRead(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var note map[string]any
	if err := client.GetJSON(ctx, "/api/notes/"+args[0], &note); err != nil {
		return fmt.Errorf("read note: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "VISIBILITY", "UPDATED", "BODY"}
		rows := [][]string{{
			truncateID(strVal(note, "id")),
			strVal(note, "title"),
			strVal(note, "visibility"),
			shortTime(strVal(note, "updated_at")),
			strVal(note, "body"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, note)
}

// ---------------------------------------------------------------------------
// comments
// ---------------------------------------------------------------------------

func runNoteComments(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var comments []map[string]any
	if err := client.GetJSON(ctx, "/api/notes/"+args[0]+"/comments", &comments); err != nil {
		return fmt.Errorf("list note comments: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "KIND", "AUTHOR", "RESOLVED", "CREATED", "BODY"}
		rows := make([][]string, 0, len(comments))
		for _, c := range comments {
			resolved := "no"
			if b, ok := c["resolved"].(bool); ok && b {
				resolved = "yes"
			}
			rows = append(rows, []string{
				truncateID(strVal(c, "id")),
				strVal(c, "kind"),
				truncateID(strVal(c, "author_id")),
				resolved,
				shortTime(strVal(c, "created_at")),
				strVal(c, "body"),
			})
		}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, comments)
}

// ---------------------------------------------------------------------------
// search
// ---------------------------------------------------------------------------

func runNoteSearch(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		return fmt.Errorf("a search query is required")
	}

	params := url.Values{}
	params.Set("q", query)
	if v, _ := cmd.Flags().GetString("kind"); v != "" {
		params.Set("kind", v)
	}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	if v, _ := cmd.Flags().GetInt("offset"); v > 0 {
		params.Set("offset", fmt.Sprintf("%d", v))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var resp struct {
		Results []map[string]any `json:"results"`
		Total   int64            `json:"total"`
	}
	if err := client.GetJSON(ctx, "/api/notes/search?"+params.Encode(), &resp); err != nil {
		return fmt.Errorf("search notes: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "KIND", "TITLE", "MATCH", "SNIPPET"}
		rows := make([][]string, 0, len(resp.Results))
		for _, res := range resp.Results {
			rows = append(rows, []string{
				truncateID(strVal(res, "id")),
				strVal(res, "kind"),
				strVal(res, "title"),
				strVal(res, "match_source"),
				strVal(res, "snippet"),
			})
		}
		cli.PrintTable(os.Stdout, headers, rows)
		fmt.Printf("\n%d result(s).\n", resp.Total)
		return nil
	}
	return cli.PrintJSON(os.Stdout, resp)
}

// shortTime trims an RFC3339 timestamp to minute precision for table display.
func shortTime(ts string) string {
	if len(ts) >= 16 {
		return ts[:16]
	}
	return ts
}
