package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var docCmd = &cobra.Command{
	Use:   "doc",
	Short: "Work with workspace documents (knowledge base)",
}

var docListCmd = &cobra.Command{
	Use:   "list",
	Short: "List documents in the workspace",
	RunE:  runDocList,
}

var docTreeCmd = &cobra.Command{
	Use:   "tree",
	Short: "Show document tree hierarchy",
	RunE:  runDocTree,
}

var docIndexCmd = &cobra.Command{
	Use:   "index",
	Short: "Show compact document index (path + description)",
	RunE:  runDocIndex,
}

var docSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search documents by content",
	Args:  exactArgs(1),
	RunE:  runDocSearch,
}

var docGrepCmd = &cobra.Command{
	Use:   "grep <regex>",
	Short: "Search documents by regex (client-side)",
	Args:  exactArgs(1),
	RunE:  runDocGrep,
}

var docGetCmd = &cobra.Command{
	Use:   "get <path>",
	Short: "Get a document by path (prints content)",
	Args:  exactArgs(1),
	RunE:  runDocGet,
}

var docShowCmd = &cobra.Command{
	Use:   "show <path>",
	Short: "Show a specific revision of a document",
	Args:  exactArgs(1),
	RunE:  runDocShow,
}

var docPutCmd = &cobra.Command{
	Use:   "put <path>",
	Short: "Create or update a document",
	Args:  exactArgs(1),
	RunE:  runDocPut,
}

var docPatchCmd = &cobra.Command{
	Use:   "patch <path>",
	Short: "Apply a fuzzy find-and-replace to a document",
	Args:  exactArgs(1),
	RunE:  runDocPatch,
}

var docRenameCmd = &cobra.Command{
	Use:   "rename <old-path> <new-path>",
	Short: "Rename a document path",
	Args:  exactArgs(2),
	RunE:  runDocRename,
}

var docPinCmd = &cobra.Command{
	Use:   "pin <path>",
	Short: "Pin a document (always injected into agent prompts)",
	Args:  exactArgs(1),
	RunE:  runDocPin,
}

var docUnpinCmd = &cobra.Command{
	Use:   "unpin <path>",
	Short: "Unpin a document",
	Args:  exactArgs(1),
	RunE:  runDocUnpin,
}

var docArchiveCmd = &cobra.Command{
	Use:   "archive <path>",
	Short: "Archive (soft-delete) a document",
	Args:  exactArgs(1),
	RunE:  runDocArchive,
}

var docHistoryCmd = &cobra.Command{
	Use:   "history <path>",
	Short: "Show revision history for a document",
	Args:  exactArgs(1),
	RunE:  runDocHistory,
}

var docDiffCmd = &cobra.Command{
	Use:   "diff <path>",
	Short: "Show diff between two revisions",
	Args:  exactArgs(1),
	RunE:  runDocDiff,
}

var docRevertCmd = &cobra.Command{
	Use:   "revert <path>",
	Short: "Revert a document to a specific revision",
	Args:  exactArgs(1),
	RunE:  runDocRevert,
}

var docTagCmd = &cobra.Command{
	Use:   "tag <path>",
	Short: "Add or remove tags on a document",
	Args:  exactArgs(1),
	RunE:  runDocTag,
}

var docLinkCmd = &cobra.Command{
	Use:   "link <issue-id> <path>",
	Short: "Link a document to an issue",
	Args:  exactArgs(2),
	RunE:  runDocLink,
}

var docUnlinkCmd = &cobra.Command{
	Use:   "unlink <issue-id> <document-id>",
	Short: "Unlink a document from an issue",
	Args:  exactArgs(2),
	RunE:  runDocUnlink,
}

func init() {
	docCmd.AddCommand(docListCmd)
	docCmd.AddCommand(docTreeCmd)
	docCmd.AddCommand(docIndexCmd)
	docCmd.AddCommand(docSearchCmd)
	docCmd.AddCommand(docGrepCmd)
	docCmd.AddCommand(docGetCmd)
	docCmd.AddCommand(docShowCmd)
	docCmd.AddCommand(docPutCmd)
	docCmd.AddCommand(docPatchCmd)
	docCmd.AddCommand(docRenameCmd)
	docCmd.AddCommand(docPinCmd)
	docCmd.AddCommand(docUnpinCmd)
	docCmd.AddCommand(docArchiveCmd)
	docCmd.AddCommand(docHistoryCmd)
	docCmd.AddCommand(docDiffCmd)
	docCmd.AddCommand(docRevertCmd)
	docCmd.AddCommand(docTagCmd)
	docCmd.AddCommand(docLinkCmd)
	docCmd.AddCommand(docUnlinkCmd)

	// doc list
	docListCmd.Flags().String("path-prefix", "", "Filter by path prefix")
	docListCmd.Flags().String("tag", "", "Filter by tag (comma-separated)")
	docListCmd.Flags().Bool("pinned", false, "Show only pinned documents")
	docListCmd.Flags().String("output", "table", "Output format: table or json")

	// doc tree
	docTreeCmd.Flags().String("path-prefix", "", "Filter by path prefix")

	// doc index
	docIndexCmd.Flags().String("path-prefix", "", "Filter by path prefix")
	docIndexCmd.Flags().String("output", "table", "Output format: table or json")

	// doc search
	docSearchCmd.Flags().Int("limit", 20, "Maximum results")
	docSearchCmd.Flags().String("output", "table", "Output format: table or json")

	// doc grep
	docGrepCmd.Flags().String("path-prefix", "", "Filter by path prefix")
	docGrepCmd.Flags().Bool("ignore-case", false, "Case-insensitive matching")
	docGrepCmd.Flags().String("output", "text", "Output format: text or json")

	// doc get
	docGetCmd.Flags().String("output", "content", "Output format: content (raw text) or json")

	// doc show
	docShowCmd.Flags().Int("rev", 0, "Revision number (required)")
	docShowCmd.Flags().String("output", "content", "Output format: content (raw text) or json")

	// doc put
	docPutCmd.Flags().Bool("content-stdin", false, "Read content from stdin")
	docPutCmd.Flags().String("title", "", "Document title")
	docPutCmd.Flags().String("description", "", "Short description")
	docPutCmd.Flags().String("tags", "", "Comma-separated tags")
	docPutCmd.Flags().String("base-revision", "", "Base revision ID for conflict detection")
	docPutCmd.Flags().String("summary", "", "Change summary")
	docPutCmd.Flags().String("output", "json", "Output format: table or json")

	// doc patch
	docPatchCmd.Flags().String("find", "", "Text to find (required)")
	docPatchCmd.Flags().String("replace", "", "Replacement text (required)")
	docPatchCmd.Flags().String("summary", "", "Change summary")
	docPatchCmd.Flags().String("output", "json", "Output format: table or json")

	// doc history
	docHistoryCmd.Flags().String("output", "table", "Output format: table or json")

	// doc diff
	docDiffCmd.Flags().Int("from", 0, "From revision number (required)")
	docDiffCmd.Flags().Int("to", 0, "To revision number (required)")

	// doc revert
	docRevertCmd.Flags().Int("to-rev", 0, "Revision number to revert to (required)")
	docRevertCmd.Flags().String("output", "json", "Output format: table or json")

	// doc tag
	docTagCmd.Flags().String("add", "", "Comma-separated tags to add")
	docTagCmd.Flags().String("remove", "", "Comma-separated tags to remove")
	docTagCmd.Flags().String("output", "json", "Output format: table or json")

	// doc link
	docLinkCmd.Flags().String("type", "referenced", "Link type: referenced, produced, or consumed")

	// doc archive
	docArchiveCmd.Flags().String("reason", "", "Archive reason")
}

// ---------------------------------------------------------------------------
// doc list
// ---------------------------------------------------------------------------

func runDocList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	query := url.Values{}
	if v, _ := cmd.Flags().GetString("path-prefix"); v != "" {
		query.Set("path-prefix", v)
	}
	if v, _ := cmd.Flags().GetString("tag"); v != "" {
		query.Set("tag", v)
	}
	if v, _ := cmd.Flags().GetBool("pinned"); v {
		query.Set("pinned", "true")
	}

	path := "/api/documents"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}

	var docs []map[string]any
	if err := client.GetJSON(ctx, path, &docs); err != nil {
		return fmt.Errorf("list documents: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, docs)
	}

	headers := []string{"PATH", "TITLE", "PINNED", "UPDATED_AT"}
	rows := make([][]string, 0, len(docs))
	for _, d := range docs {
		pinned := ""
		if b, ok := d["pinned"].(bool); ok && b {
			pinned = "yes"
		}
		rows = append(rows, []string{
			strVal(d, "path"),
			strVal(d, "title"),
			pinned,
			strVal(d, "updated_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// doc tree
// ---------------------------------------------------------------------------

func runDocTree(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	path := "/api/documents/tree"
	if v, _ := cmd.Flags().GetString("path-prefix"); v != "" {
		path += "?path-prefix=" + url.QueryEscape(v)
	}

	// Get raw text response.
	resp, err := client.GetRaw(ctx, path)
	if err != nil {
		return fmt.Errorf("get tree: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	fmt.Print(string(body))
	return nil
}

// ---------------------------------------------------------------------------
// doc index
// ---------------------------------------------------------------------------

func runDocIndex(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var entries []map[string]any
	if err := client.GetJSON(ctx, "/api/documents/index", &entries); err != nil {
		return fmt.Errorf("get index: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, entries)
	}

	headers := []string{"PATH", "DESCRIPTION", "PINNED"}
	rows := make([][]string, 0, len(entries))
	for _, e := range entries {
		pinned := ""
		if b, ok := e["pinned"].(bool); ok && b {
			pinned = "yes"
		}
		rows = append(rows, []string{
			strVal(e, "path"),
			strVal(e, "description"),
			pinned,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// doc search
// ---------------------------------------------------------------------------

func runDocSearch(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	limit, _ := cmd.Flags().GetInt("limit")
	query := url.Values{}
	query.Set("q", args[0])
	query.Set("limit", fmt.Sprintf("%d", limit))

	var results []map[string]any
	if err := client.GetJSON(ctx, "/api/documents/search?"+query.Encode(), &results); err != nil {
		return fmt.Errorf("search documents: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, results)
	}

	headers := []string{"PATH", "TITLE", "DESCRIPTION", "RANK"}
	rows := make([][]string, 0, len(results))
	for _, r := range results {
		rows = append(rows, []string{
			strVal(r, "path"),
			strVal(r, "title"),
			strVal(r, "description"),
			strVal(r, "rank"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// doc grep (client-side regex search)
// ---------------------------------------------------------------------------

func runDocGrep(cmd *cobra.Command, args []string) error {
	ignoreCase, _ := cmd.Flags().GetBool("ignore-case")

	pattern := args[0]
	if ignoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid regex: %w", err)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	query := url.Values{}
	if v, _ := cmd.Flags().GetString("path-prefix"); v != "" {
		query.Set("path-prefix", v)
	}

	path := "/api/documents"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}

	var docs []map[string]any
	if err := client.GetJSON(ctx, path, &docs); err != nil {
		return fmt.Errorf("list documents: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")

	type grepMatch struct {
		Path    string `json:"path"`
		Line    int    `json:"line"`
		Content string `json:"content"`
	}

	var matches []grepMatch
	for _, d := range docs {
		docPath := strVal(d, "path")
		content := strVal(d, "content")
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				matches = append(matches, grepMatch{
					Path:    docPath,
					Line:    i + 1,
					Content: line,
				})
			}
		}
	}

	if output == "json" {
		return cli.PrintJSON(os.Stdout, matches)
	}

	for _, m := range matches {
		fmt.Printf("%s:%d:%s\n", m.Path, m.Line, m.Content)
	}
	return nil
}

// ---------------------------------------------------------------------------
// doc get
// ---------------------------------------------------------------------------

func runDocGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var doc map[string]any
	if err := client.GetJSON(ctx, "/api/documents/by-path/"+args[0], &doc); err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, doc)
	}

	// content mode: print raw content
	content := strVal(doc, "content")
	fmt.Print(content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		fmt.Println()
	}
	return nil
}

// ---------------------------------------------------------------------------
// doc show (specific revision)
// ---------------------------------------------------------------------------

func runDocShow(cmd *cobra.Command, args []string) error {
	revNum, _ := cmd.Flags().GetInt("rev")
	if revNum < 1 {
		return fmt.Errorf("--rev is required and must be >= 1")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// First get the document to find its ID.
	var doc map[string]any
	if err := client.GetJSON(ctx, "/api/documents/by-path/"+args[0], &doc); err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	docID := strVal(doc, "id")
	var rev map[string]any
	if err := client.GetJSON(ctx, fmt.Sprintf("/api/documents/%s/revisions/%d", docID, revNum), &rev); err != nil {
		return fmt.Errorf("get revision: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, rev)
	}

	content := strVal(rev, "content")
	fmt.Print(content)
	if content != "" && !strings.HasSuffix(content, "\n") {
		fmt.Println()
	}
	return nil
}

// ---------------------------------------------------------------------------
// doc put
// ---------------------------------------------------------------------------

func runDocPut(cmd *cobra.Command, args []string) error {
	useStdin, _ := cmd.Flags().GetBool("content-stdin")
	if !useStdin {
		return fmt.Errorf("--content-stdin is required (pipe content via stdin)")
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	content := strings.TrimSuffix(string(data), "\n")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{
		"content": content,
	}
	if v, _ := cmd.Flags().GetString("title"); v != "" {
		body["title"] = v
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	if v, _ := cmd.Flags().GetString("tags"); v != "" {
		body["tags"] = strings.Split(v, ",")
	}
	if v, _ := cmd.Flags().GetString("base-revision"); v != "" {
		body["base_revision_id"] = v
	}
	if v, _ := cmd.Flags().GetString("summary"); v != "" {
		body["change_summary"] = v
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/documents/by-path/"+args[0], body, &result); err != nil {
		return fmt.Errorf("put document: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Document saved: %s (%s)\n", strVal(result, "path"), strVal(result, "id"))
	return nil
}

// ---------------------------------------------------------------------------
// doc patch
// ---------------------------------------------------------------------------

func runDocPatch(cmd *cobra.Command, args []string) error {
	findText, _ := cmd.Flags().GetString("find")
	replaceText, _ := cmd.Flags().GetString("replace")
	if findText == "" {
		return fmt.Errorf("--find is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// First get the document ID by path.
	var doc map[string]any
	if err := client.GetJSON(ctx, "/api/documents/by-path/"+args[0], &doc); err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	docID := strVal(doc, "id")
	body := map[string]any{
		"find":    findText,
		"replace": replaceText,
	}
	if v, _ := cmd.Flags().GetString("summary"); v != "" {
		body["change_summary"] = v
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/documents/"+docID+"/patch", body, &result); err != nil {
		return fmt.Errorf("patch document: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Document patched: %s\n", strVal(result, "path"))
	return nil
}

// ---------------------------------------------------------------------------
// doc rename
// ---------------------------------------------------------------------------

func runDocRename(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Get document by old path.
	var doc map[string]any
	if err := client.GetJSON(ctx, "/api/documents/by-path/"+args[0], &doc); err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	docID := strVal(doc, "id")
	body := map[string]any{
		"new_path": args[1],
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/documents/"+docID+"/rename", body, &result); err != nil {
		return fmt.Errorf("rename document: %w", err)
	}

	fmt.Printf("Document renamed: %s → %s\n", args[0], args[1])
	return nil
}

// ---------------------------------------------------------------------------
// doc pin / unpin
// ---------------------------------------------------------------------------

func runDocPin(cmd *cobra.Command, args []string) error {
	return setDocPinned(cmd, args[0], true)
}

func runDocUnpin(cmd *cobra.Command, args []string) error {
	return setDocPinned(cmd, args[0], false)
}

func setDocPinned(cmd *cobra.Command, path string, pin bool) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var doc map[string]any
	if err := client.GetJSON(ctx, "/api/documents/by-path/"+path, &doc); err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	docID := strVal(doc, "id")
	action := "pin"
	if !pin {
		action = "unpin"
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/documents/"+docID+"/"+action, nil, &result); err != nil {
		return fmt.Errorf("%s document: %w", action, err)
	}

	if pin {
		fmt.Printf("Document pinned: %s\n", path)
	} else {
		fmt.Printf("Document unpinned: %s\n", path)
	}
	return nil
}

// ---------------------------------------------------------------------------
// doc archive
// ---------------------------------------------------------------------------

func runDocArchive(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var doc map[string]any
	if err := client.GetJSON(ctx, "/api/documents/by-path/"+args[0], &doc); err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	docID := strVal(doc, "id")
	body := map[string]any{}
	if v, _ := cmd.Flags().GetString("reason"); v != "" {
		body["reason"] = v
	}

	var result any
	if err := client.PostJSON(ctx, "/api/documents/"+docID+"/archive", body, &result); err != nil {
		return fmt.Errorf("archive document: %w", err)
	}

	fmt.Printf("Document archived: %s\n", args[0])
	return nil
}

// ---------------------------------------------------------------------------
// doc history
// ---------------------------------------------------------------------------

func runDocHistory(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var doc map[string]any
	if err := client.GetJSON(ctx, "/api/documents/by-path/"+args[0], &doc); err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	docID := strVal(doc, "id")
	var revisions []map[string]any
	if err := client.GetJSON(ctx, "/api/documents/"+docID+"/revisions", &revisions); err != nil {
		return fmt.Errorf("list revisions: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, revisions)
	}

	headers := []string{"REV", "OPERATION", "AUTHOR_TYPE", "SUMMARY", "CREATED_AT"}
	rows := make([][]string, 0, len(revisions))
	for _, r := range revisions {
		rows = append(rows, []string{
			strVal(r, "revision_number"),
			strVal(r, "operation"),
			strVal(r, "author_type"),
			strVal(r, "change_summary"),
			strVal(r, "created_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// doc diff
// ---------------------------------------------------------------------------

func runDocDiff(cmd *cobra.Command, args []string) error {
	fromRev, _ := cmd.Flags().GetInt("from")
	toRev, _ := cmd.Flags().GetInt("to")
	if fromRev < 1 || toRev < 1 {
		return fmt.Errorf("--from and --to are required and must be >= 1")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var doc map[string]any
	if err := client.GetJSON(ctx, "/api/documents/by-path/"+args[0], &doc); err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	docID := strVal(doc, "id")

	var revA, revB map[string]any
	if err := client.GetJSON(ctx, fmt.Sprintf("/api/documents/%s/revisions/%d", docID, fromRev), &revA); err != nil {
		return fmt.Errorf("get revision %d: %w", fromRev, err)
	}
	if err := client.GetJSON(ctx, fmt.Sprintf("/api/documents/%s/revisions/%d", docID, toRev), &revB); err != nil {
		return fmt.Errorf("get revision %d: %w", toRev, err)
	}

	contentA := strVal(revA, "content")
	contentB := strVal(revB, "content")

	// Simple line-by-line diff.
	linesA := strings.Split(contentA, "\n")
	linesB := strings.Split(contentB, "\n")

	fmt.Printf("--- revision %d\n", fromRev)
	fmt.Printf("+++ revision %d\n", toRev)

	maxLines := len(linesA)
	if len(linesB) > maxLines {
		maxLines = len(linesB)
	}

	for i := 0; i < maxLines; i++ {
		var lineA, lineB string
		if i < len(linesA) {
			lineA = linesA[i]
		}
		if i < len(linesB) {
			lineB = linesB[i]
		}
		if lineA != lineB {
			if i < len(linesA) {
				fmt.Printf("- %s\n", lineA)
			}
			if i < len(linesB) {
				fmt.Printf("+ %s\n", lineB)
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// doc revert
// ---------------------------------------------------------------------------

func runDocRevert(cmd *cobra.Command, args []string) error {
	revNum, _ := cmd.Flags().GetInt("to-rev")
	if revNum < 1 {
		return fmt.Errorf("--to-rev is required and must be >= 1")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var doc map[string]any
	if err := client.GetJSON(ctx, "/api/documents/by-path/"+args[0], &doc); err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	docID := strVal(doc, "id")
	body := map[string]any{
		"revision_number": revNum,
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/documents/"+docID+"/restore", body, &result); err != nil {
		return fmt.Errorf("revert document: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Document reverted to revision %d: %s\n", revNum, args[0])
	return nil
}

// ---------------------------------------------------------------------------
// doc tag
// ---------------------------------------------------------------------------

func runDocTag(cmd *cobra.Command, args []string) error {
	addStr, _ := cmd.Flags().GetString("add")
	removeStr, _ := cmd.Flags().GetString("remove")

	if addStr == "" && removeStr == "" {
		return fmt.Errorf("at least one of --add or --remove is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Get document by path to find its ID.
	var doc map[string]any
	if err := client.GetJSON(ctx, "/api/documents/by-path/"+args[0], &doc); err != nil {
		return fmt.Errorf("get document: %w", err)
	}

	docID := strVal(doc, "id")
	body := map[string]any{}
	if addStr != "" {
		body["add"] = strings.Split(addStr, ",")
	}
	if removeStr != "" {
		body["remove"] = strings.Split(removeStr, ",")
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/documents/"+docID+"/tags", body, &result); err != nil {
		return fmt.Errorf("update tags: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Tags updated: %s\n", args[0])
	return nil
}

// ---------------------------------------------------------------------------
// doc link / unlink
// ---------------------------------------------------------------------------

func runDocLink(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	linkType, _ := cmd.Flags().GetString("type")

	body := map[string]any{
		"path":      args[1],
		"link_type": linkType,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var result any
	if err := client.PostJSON(ctx, "/api/issues/"+args[0]+"/documents/links", body, &result); err != nil {
		return fmt.Errorf("link document: %w", err)
	}

	fmt.Printf("Document linked: %s → issue %s (type: %s)\n", args[1], args[0], linkType)
	return nil
}

func runDocUnlink(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/issues/"+args[0]+"/documents/links/"+args[1]); err != nil {
		return fmt.Errorf("unlink document: %w", err)
	}

	fmt.Printf("Document unlinked from issue %s\n", args[0])
	return nil
}
