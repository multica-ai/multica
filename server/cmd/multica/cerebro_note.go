// CEREBRO-PATCH(cerebro-note-cli): FIR-2022 cerebro-only file — read + search
// notes/documents and their comments from the CLI, plus the FIR-2821 write path
// (comment add/reply/resolve, comment send) and granular note↔issue coupling
// (reference add/list). The server surface lives in server/internal/cerebro/note
// (GET/POST/PUT/DELETE /api/notes/{id}, /{id}/comments, /{id}/references, /search).
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
	Use:        "comments <id>",
	Short:      "List the comments on a note/document",
	Args:       exactArgs(1),
	RunE:       runNoteComments,
	Deprecated: `use "note comment list" instead`,
}

var noteSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search notes/documents by title, body and comments",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runNoteSearch,
}

// noteCommentCmd is the `multica note comment ...` write-path group (FIR-2821):
// add/reply, resolve/reopen, and send comments to the coupled destination. The
// singular `comment` is the canonical group; the plural `note comments` list
// stays as a deprecated alias for `note comment list`.
var noteCommentCmd = &cobra.Command{
	Use:   "comment",
	Short: "Read and write comments on a note/document",
}

var noteCommentListCmd = &cobra.Command{
	Use:   "list <note-id>",
	Short: "List the comments on a note/document",
	Args:  exactArgs(1),
	RunE:  runNoteComments,
}

var noteCommentAddCmd = &cobra.Command{
	Use:   "add <note-id>",
	Short: "Add a comment on a note/document (or reply to a thread with --reply-to)",
	Args:  exactArgs(1),
	RunE:  runNoteCommentAdd,
}

var noteCommentResolveCmd = &cobra.Command{
	Use:   "resolve <note-id> <comment-id>",
	Short: "Resolve a comment thread (use --reopen to reopen it)",
	Args:  exactArgs(2),
	RunE:  runNoteCommentResolve,
}

var noteCommentSendCmd = &cobra.Command{
	Use:   "send <note-id>",
	Short: "Send the note's agent-tagged comments to its coupled issue/chat",
	Args:  exactArgs(1),
	RunE:  runNoteCommentSend,
}

// noteReferenceCmd is the `multica note reference ...` coupling group (FIR-2821):
// link a note to one or more objects (issues today) and list those links. The
// coupling is what `note comment send` dispatches to.
var noteReferenceCmd = &cobra.Command{
	Use:     "reference",
	Aliases: []string{"ref"},
	Short:   "Couple a note/document to issues (and list its couplings)",
}

var noteReferenceListCmd = &cobra.Command{
	Use:   "list <note-id>",
	Short: "List the references (couplings) on a note/document",
	Args:  exactArgs(1),
	RunE:  runNoteReferenceList,
}

var noteReferenceAddCmd = &cobra.Command{
	Use:   "add <note-id>",
	Short: "Couple a note/document to an issue (or another object)",
	Args:  exactArgs(1),
	RunE:  runNoteReferenceAdd,
}

func init() {
	noteCmd.AddCommand(noteReadCmd)
	noteCmd.AddCommand(noteCommentsCmd)
	noteCmd.AddCommand(noteSearchCmd)
	noteCmd.AddCommand(noteCommentCmd)
	noteCmd.AddCommand(noteReferenceCmd)

	noteReadCmd.Flags().String("output", "json", "Output format: table or json")
	noteCommentsCmd.Flags().String("output", "json", "Output format: table or json")

	noteSearchCmd.Flags().String("kind", "", "Restrict to a kind: report|plan|decision|diagram|note")
	noteSearchCmd.Flags().Int("limit", 20, "Maximum number of results (max 50)")
	noteSearchCmd.Flags().Int("offset", 0, "Pagination offset")
	noteSearchCmd.Flags().String("output", "json", "Output format: table or json")

	// note comment ...
	noteCommentCmd.AddCommand(noteCommentListCmd)
	noteCommentCmd.AddCommand(noteCommentAddCmd)
	noteCommentCmd.AddCommand(noteCommentResolveCmd)
	noteCommentCmd.AddCommand(noteCommentSendCmd)

	noteCommentListCmd.Flags().String("output", "json", "Output format: table or json")

	noteCommentAddCmd.Flags().String("body", "", "Comment body (required)")
	noteCommentAddCmd.Flags().String("reply-to", "", "Reply to an existing comment: its id becomes the thread root")
	noteCommentAddCmd.Flags().String("output", "json", "Output format: table or json")

	noteCommentResolveCmd.Flags().Bool("reopen", false, "Reopen the thread instead of resolving it")
	noteCommentResolveCmd.Flags().String("output", "json", "Output format: table or json")

	noteCommentSendCmd.Flags().StringArray("comment", nil, "Comment id to send; repeat for several. Omit to send all unsent, agent-tagged comments")
	noteCommentSendCmd.Flags().String("destination-object", "", "Disambiguate destination when the note is coupled to several: issue|chat_session")
	noteCommentSendCmd.Flags().String("destination-ref-id", "", "Disambiguate destination when the note is coupled to several: the chosen object's ref id")
	noteCommentSendCmd.Flags().String("output", "json", "Output format: table or json")

	// note reference ...
	noteReferenceCmd.AddCommand(noteReferenceListCmd)
	noteReferenceCmd.AddCommand(noteReferenceAddCmd)

	noteReferenceListCmd.Flags().String("output", "json", "Output format: table or json")

	noteReferenceAddCmd.Flags().String("issue", "", "Couple to an issue by key or id (shortcut for --object issue --ref-id <resolved-uuid>)")
	noteReferenceAddCmd.Flags().String("object", "", "Object kind to couple to, e.g. issue (required unless --issue is given)")
	noteReferenceAddCmd.Flags().String("ref-id", "", "Id of the object to couple to (required unless --issue is given)")
	noteReferenceAddCmd.Flags().String("type", "", "Optional reference sub-type")
	noteReferenceAddCmd.Flags().String("label", "", "Optional human label for the coupling")
	noteReferenceAddCmd.Flags().String("url", "", "Optional URL for the coupling")
	noteReferenceAddCmd.Flags().String("output", "json", "Output format: table or json")
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

// ---------------------------------------------------------------------------
// comment add / reply
// ---------------------------------------------------------------------------

func runNoteCommentAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	body, _ := cmd.Flags().GetString("body")
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("--body is required")
	}
	payload := map[string]any{"body": body}
	if replyTo, _ := cmd.Flags().GetString("reply-to"); strings.TrimSpace(replyTo) != "" {
		payload["thread_root_id"] = strings.TrimSpace(replyTo)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var created map[string]any
	if err := client.PostJSON(ctx, "/api/notes/"+args[0]+"/comments", payload, &created); err != nil {
		return fmt.Errorf("add note comment: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printCommentTable([]map[string]any{created})
		return nil
	}
	return cli.PrintJSON(os.Stdout, created)
}

// ---------------------------------------------------------------------------
// comment resolve / reopen
// ---------------------------------------------------------------------------

func runNoteCommentResolve(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	reopen, _ := cmd.Flags().GetBool("reopen")
	payload := map[string]any{"resolved": !reopen}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/notes/" + args[0] + "/comments/" + args[1] + "/resolve"
	var updated map[string]any
	if err := client.PostJSON(ctx, path, payload, &updated); err != nil {
		return fmt.Errorf("resolve note comment: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		printCommentTable([]map[string]any{updated})
		return nil
	}
	return cli.PrintJSON(os.Stdout, updated)
}

// ---------------------------------------------------------------------------
// comment send
// ---------------------------------------------------------------------------

func runNoteCommentSend(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	payload := map[string]any{}
	if ids, _ := cmd.Flags().GetStringArray("comment"); len(ids) > 0 {
		payload["comment_ids"] = ids
	}
	if v, _ := cmd.Flags().GetString("destination-object"); strings.TrimSpace(v) != "" {
		payload["destination_object"] = strings.TrimSpace(v)
	}
	if v, _ := cmd.Flags().GetString("destination-ref-id"); strings.TrimSpace(v) != "" {
		payload["destination_ref_id"] = strings.TrimSpace(v)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var resp struct {
		Sent             []map[string]any `json:"sent"`
		UnsentRemaining  int64            `json:"unsent_remaining"`
		DestinationKind  string           `json:"destination_kind"`
		DestinationRefID string           `json:"destination_ref_id"`
		AgentsTriggered  int              `json:"agents_triggered"`
	}
	if err := client.PostJSON(ctx, "/api/notes/"+args[0]+"/comments/send", payload, &resp); err != nil {
		return fmt.Errorf("send note comments: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		fmt.Printf("Sent %d comment(s) to %s %s — %d agent(s) triggered, %d draft(s) remaining.\n",
			len(resp.Sent), resp.DestinationKind, truncateID(resp.DestinationRefID), resp.AgentsTriggered, resp.UnsentRemaining)
		return nil
	}
	return cli.PrintJSON(os.Stdout, resp)
}

// ---------------------------------------------------------------------------
// reference list
// ---------------------------------------------------------------------------

func runNoteReferenceList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var resp struct {
		References []map[string]any `json:"references"`
	}
	if err := client.GetJSON(ctx, "/api/notes/"+args[0]+"/references", &resp); err != nil {
		return fmt.Errorf("list note references: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "OBJECT", "REF_ID", "LABEL", "CREATED"}
		rows := make([][]string, 0, len(resp.References))
		for _, ref := range resp.References {
			rows = append(rows, []string{
				truncateID(strVal(ref, "id")),
				strVal(ref, "object"),
				truncateID(strVal(ref, "ref_id")),
				strVal(ref, "label"),
				shortTime(strVal(ref, "created_at")),
			})
		}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, resp)
}

// ---------------------------------------------------------------------------
// reference add
// ---------------------------------------------------------------------------

func runNoteReferenceAdd(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	object, _ := cmd.Flags().GetString("object")
	refID, _ := cmd.Flags().GetString("ref-id")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// --issue is a convenience: resolve an issue key/id/prefix to its UUID and
	// pin object=issue, so agents can couple by "FIR-2821" without a manual lookup.
	if issueRef, _ := cmd.Flags().GetString("issue"); strings.TrimSpace(issueRef) != "" {
		if object != "" || refID != "" {
			return fmt.Errorf("--issue cannot be combined with --object/--ref-id")
		}
		resolved, err := resolveIssueRef(ctx, client, issueRef)
		if err != nil {
			return fmt.Errorf("resolve issue %q: %w", issueRef, err)
		}
		object = "issue"
		refID = resolved.ID
	}

	object = strings.TrimSpace(object)
	refID = strings.TrimSpace(refID)
	if object == "" || refID == "" {
		return fmt.Errorf("either --issue, or both --object and --ref-id, are required")
	}

	payload := map[string]any{"object": object, "ref_id": refID}
	if v, _ := cmd.Flags().GetString("type"); strings.TrimSpace(v) != "" {
		payload["type"] = strings.TrimSpace(v)
	}
	if v, _ := cmd.Flags().GetString("label"); strings.TrimSpace(v) != "" {
		payload["label"] = strings.TrimSpace(v)
	}
	if v, _ := cmd.Flags().GetString("url"); strings.TrimSpace(v) != "" {
		payload["url"] = strings.TrimSpace(v)
	}

	var created map[string]any
	if err := client.PostJSON(ctx, "/api/notes/"+args[0]+"/references", payload, &created); err != nil {
		return fmt.Errorf("add note reference: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "OBJECT", "REF_ID", "LABEL"}
		rows := [][]string{{
			truncateID(strVal(created, "id")),
			strVal(created, "object"),
			truncateID(strVal(created, "ref_id")),
			strVal(created, "label"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}
	return cli.PrintJSON(os.Stdout, created)
}

// printCommentTable renders one or more comment objects as a table, matching the
// column layout of `note comment list` so add/resolve output reads the same.
func printCommentTable(comments []map[string]any) {
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
}

// shortTime trims an RFC3339 timestamp to minute precision for table display.
func shortTime(ts string) string {
	if len(ts) >= 16 {
		return ts[:16]
	}
	return ts
}
