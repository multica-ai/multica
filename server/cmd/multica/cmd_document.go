package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Documents are markdown notes stored as artifacts (kind="note", format="md").
// The /<workspace-slug>/documents UI surfaces every artifact regardless of kind,
// so list/get/update/delete pass through unchanged; create defaults to a note.

var documentCmd = &cobra.Command{
	Use:   "document",
	Short: "Work with documents",
}

var documentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List documents in the workspace",
	RunE:  runDocumentList,
}

var documentGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get document details",
	Args:  exactArgs(1),
	RunE:  runDocumentGet,
}

var documentCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new document",
	RunE:  runDocumentCreate,
}

var documentUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a document",
	Args:  exactArgs(1),
	RunE:  runDocumentUpdate,
}

var documentDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a document",
	Args:  exactArgs(1),
	RunE:  runDocumentDelete,
}

func init() {
	documentCmd.AddCommand(documentListCmd)
	documentCmd.AddCommand(documentGetCmd)
	documentCmd.AddCommand(documentCreateCmd)
	documentCmd.AddCommand(documentUpdateCmd)
	documentCmd.AddCommand(documentDeleteCmd)

	documentListCmd.Flags().String("folder", "", "Filter by folder ID (filtered client-side)")
	documentListCmd.Flags().Int("limit", 50, "Maximum number of documents to return")
	documentListCmd.Flags().Int("offset", 0, "Pagination offset")
	documentListCmd.Flags().String("output", "table", "Output format: table or json")

	documentGetCmd.Flags().String("output", "json", "Output format: table or json")

	documentCreateCmd.Flags().String("title", "", "Document title (required)")
	documentCreateCmd.Flags().String("content", "", "Markdown content (mutually exclusive with --content-stdin)")
	documentCreateCmd.Flags().Bool("content-stdin", false, "Read markdown content from stdin (avoids shell escaping issues)")
	documentCreateCmd.Flags().String("folder", "", "Place the document inside this folder ID")
	documentCreateCmd.Flags().String("output", "json", "Output format: table or json")

	documentUpdateCmd.Flags().String("title", "", "New title")
	documentUpdateCmd.Flags().String("content", "", "New markdown content (mutually exclusive with --content-stdin)")
	documentUpdateCmd.Flags().Bool("content-stdin", false, "Read new markdown content from stdin")
	documentUpdateCmd.Flags().String("output", "json", "Output format: table or json")
}

// documentView is the CLI-facing shape: a curated subset of the artifact API
// response, with field names that match the document domain (creator_id /
// content rather than the artifact-API author_id / body).
type documentView struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	FolderID  *string `json:"folder_id"`
	Content   string  `json:"content"`
	CreatorID string  `json:"creator_id"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func toDocumentView(a map[string]any) documentView {
	view := documentView{
		ID:        strVal(a, "id"),
		Title:     strVal(a, "title"),
		Content:   strVal(a, "body"),
		CreatorID: strVal(a, "author_id"),
		CreatedAt: strVal(a, "created_at"),
		UpdatedAt: strVal(a, "updated_at"),
	}
	if v, ok := a["folder_id"]; ok && v != nil {
		s := fmt.Sprintf("%v", v)
		view.FolderID = &s
	}
	return view
}

// ---------------------------------------------------------------------------
// list
// ---------------------------------------------------------------------------

func runDocumentList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	limit, _ := cmd.Flags().GetInt("limit")
	offset, _ := cmd.Flags().GetInt("offset")
	folderFilter, _ := cmd.Flags().GetString("folder")

	// The /api/artifacts search endpoint has no folder filter, so when one is
	// requested we fetch a generous page and post-filter — same approach the
	// web file manager takes (see file-manager-page.tsx).
	fetchLimit := limit
	fetchOffset := offset
	if folderFilter != "" {
		fetchLimit = 200
		fetchOffset = 0
	}

	params := url.Values{}
	if fetchLimit > 0 {
		params.Set("limit", fmt.Sprintf("%d", fetchLimit))
	}
	if fetchOffset > 0 {
		params.Set("offset", fmt.Sprintf("%d", fetchOffset))
	}
	path := "/api/artifacts"
	if encoded := params.Encode(); encoded != "" {
		path += "?" + encoded
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var artifacts []map[string]any
	if err := client.GetJSON(ctx, path, &artifacts); err != nil {
		return fmt.Errorf("list documents: %w", err)
	}

	// Client-side folder filter and pagination when requested.
	if folderFilter != "" {
		filtered := make([]map[string]any, 0, len(artifacts))
		for _, a := range artifacts {
			if strVal(a, "folder_id") == folderFilter {
				filtered = append(filtered, a)
			}
		}
		artifacts = filtered
		if offset > 0 {
			if offset >= len(artifacts) {
				artifacts = artifacts[:0]
			} else {
				artifacts = artifacts[offset:]
			}
		}
		if limit > 0 && len(artifacts) > limit {
			artifacts = artifacts[:limit]
		}
	}

	views := make([]documentView, len(artifacts))
	for i, a := range artifacts {
		views[i] = toDocumentView(a)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{
			"documents": views,
			"total":     len(views),
			"limit":     limit,
			"offset":    offset,
		})
	}

	headers := []string{"ID", "TITLE", "FOLDER", "CREATOR", "UPDATED"}
	rows := make([][]string, 0, len(views))
	for _, v := range views {
		folder := "—"
		if v.FolderID != nil {
			folder = truncateID(*v.FolderID)
		}
		updated := v.UpdatedAt
		if len(updated) >= 16 {
			updated = updated[:16]
		}
		rows = append(rows, []string{
			truncateID(v.ID),
			v.Title,
			folder,
			truncateID(v.CreatorID),
			updated,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// get
// ---------------------------------------------------------------------------

func runDocumentGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var artifact map[string]any
	if err := client.GetJSON(ctx, "/api/artifacts/"+args[0], &artifact); err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	view := toDocumentView(artifact)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		folder := ""
		if view.FolderID != nil {
			folder = *view.FolderID
		}
		headers := []string{"ID", "TITLE", "FOLDER", "CREATOR", "UPDATED", "CONTENT"}
		rows := [][]string{{
			truncateID(view.ID),
			view.Title,
			truncateID(folder),
			truncateID(view.CreatorID),
			view.UpdatedAt,
			view.Content,
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, view)
}

// ---------------------------------------------------------------------------
// create
// ---------------------------------------------------------------------------

func runDocumentCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}

	content, _ := cmd.Flags().GetString("content")
	useStdin, _ := cmd.Flags().GetBool("content-stdin")
	if content != "" && useStdin {
		return fmt.Errorf("--content and --content-stdin are mutually exclusive")
	}
	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		content = strings.TrimSuffix(string(data), "\n")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{
		"kind":   "note",
		"format": "md",
		"title":  title,
		"body":   content,
	}
	if v, _ := cmd.Flags().GetString("folder"); v != "" {
		body["folder_id"] = v
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/artifacts", body, &result); err != nil {
		return fmt.Errorf("create document: %w", err)
	}

	view := toDocumentView(result)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		fmt.Printf("Document created: %s (%s)\n", view.Title, view.ID)
		return nil
	}
	return cli.PrintJSON(os.Stdout, view)
}

// ---------------------------------------------------------------------------
// update
// ---------------------------------------------------------------------------

func runDocumentUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	useStdin, _ := cmd.Flags().GetBool("content-stdin")
	contentSet := cmd.Flags().Changed("content")
	if contentSet && useStdin {
		return fmt.Errorf("--content and --content-stdin are mutually exclusive")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{}
	if cmd.Flags().Changed("title") {
		v, _ := cmd.Flags().GetString("title")
		body["title"] = v
	}
	if contentSet {
		v, _ := cmd.Flags().GetString("content")
		body["body"] = v
	}
	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		body["body"] = strings.TrimSuffix(string(data), "\n")
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --title, --content, or --content-stdin")
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/artifacts/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update document: %w", err)
	}

	view := toDocumentView(result)

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		fmt.Printf("Document updated: %s (%s)\n", view.Title, view.ID)
		return nil
	}
	return cli.PrintJSON(os.Stdout, view)
}

// ---------------------------------------------------------------------------
// delete
// ---------------------------------------------------------------------------

func runDocumentDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/artifacts/"+args[0]); err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	fmt.Printf("Document %s deleted.\n", args[0])
	return nil
}
