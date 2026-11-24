package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Doc commands — the workspace knowledge base (MUL-8370).
//
// Writes are optimistic: `doc update` must name the revision it edited
// (`--base-revision`, from `doc get`). When someone else saved in between,
// the server merges the change into theirs, or answers 409 when both changed
// the same paragraph. `doc edit` replaces quoted text and needs no revision.

var docCmd = &cobra.Command{
	Use:   "doc",
	Short: "Work with knowledge base docs",
}

var docListCmd = &cobra.Command{
	Use:   "list",
	Short: "List docs and folders",
	RunE:  runDocList,
}

var docSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search docs by title or content",
	Long: "Search docs in the current workspace. The query matches doc titles and\n" +
		"Markdown content; with several words, every word must appear somewhere\n" +
		"in the doc. Title matches rank first.\n\n" +
		"The MATCH column (match_source in --output json) reports whether the\n" +
		"title or the content matched, with a short excerpt for content hits.\n" +
		"Read a result with `multica doc get <doc-id>`.",
	Args: exactArgs(1),
	RunE: runDocSearch,
}

var docMoveCmd = &cobra.Command{
	Use:   "move <doc-id>",
	Short: "Move a doc into a folder, or to the top level",
	Long: "Move a doc into another folder of the workspace knowledge base.\n\n" +
		"--folder takes a folder id (or a unique prefix of one from `multica doc list`), " +
		"or `root` for the top level. Moving does not change the doc's content or revision.",
	Args: exactArgs(1),
	RunE: runDocMove,
}

var docGetCmd = &cobra.Command{
	Use:   "get <doc-id>",
	Short: "Get a doc with its content and current revision",
	Args:  exactArgs(1),
	RunE:  runDocGet,
}

var docCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a doc",
	RunE:  runDocCreate,
}

var docUpdateCmd = &cobra.Command{
	Use:   "update <doc-id>",
	Short: "Update a doc's title or content",
	Long: "Replace a doc's title or its whole content.\n\n" +
		"People may be editing a doc while you work on it. For targeted changes prefer " +
		"`multica doc edit`: it replaces text you quote and lands wherever that text now is. " +
		"--base-revision is the revision you read with `multica doc get`; edits made since then are usually merged in. " +
		"The update is refused, with nothing written, when you and someone else changed the same paragraph, " +
		"and also when your revision is too old to merge from or the live-editing service is unavailable. " +
		"On a refusal, run `multica doc get` and redo your change on that content; never resend the same body " +
		"with the new revision, which would erase their edits. " +
		"Pass --message to say what changed and why — it shows in the doc's history.",
	Args: exactArgs(1),
	RunE: runDocUpdate,
}

var docEditCmd = &cobra.Command{
	Use:   "edit <doc-id>",
	Short: "Replace quoted text in a doc",
	Long: "Replace text in a doc without rewriting the rest of it.\n\n" +
		"--old must match the doc's current Markdown exactly once (copy it from `multica doc get --output content`, " +
		"with enough surrounding text to be unique), or at least once with --replace-all. " +
		"The edit applies to the live doc, so it is safe while people are editing; " +
		"it fails, with nothing written, only when the quoted text is gone or, without --replace-all, appears more than once.",
	Args: exactArgs(1),
	RunE: runDocEdit,
}

var docHistoryCmd = &cobra.Command{
	Use:   "history <doc-id>",
	Short: "List a doc's versions, or show one with --version",
	Long: "List the saved versions of a doc, newest first: who saved each one, its action " +
		"(edit or restore) and the change summary. --output json adds the run and issue an agent saved it from.\n\n" +
		"Pass --version <n> to get that version with its full content (`--output content` prints only the Markdown), " +
		"for example to compare it with `multica doc get --output content` or to pick one for `multica doc restore`.",
	Args: exactArgs(1),
	RunE: runDocHistory,
}

var docRestoreCmd = &cobra.Command{
	Use:   "restore <doc-id>",
	Short: "Restore a doc to an earlier version",
	Long: "Replace a doc's title and content with an earlier version from `multica doc history`.\n\n" +
		"The restore is saved as a new version, so nothing is lost: the content it replaces stays in the history. " +
		"It replaces the whole doc, including edits made after the version you restore; " +
		"to undo one change and keep the rest, use `multica doc edit` instead. " +
		"--base-revision is the revision from `multica doc get`; when the doc changed after it, " +
		"live edits included, nothing is restored.",
	Args: exactArgs(1),
	RunE: runDocRestore,
}

var docCommentCmd = &cobra.Command{
	Use:   "comment",
	Short: "Read and reply to the discussion on a doc",
	Long: "Read the comment threads people left on a doc, and reply in them. " +
		"Starting a discussion, resolving one and deleting comments are reserved for people.",
}

var docCommentAddCmd = &cobra.Command{
	Use:   "add <doc-id>",
	Short: "Reply in a doc discussion",
	Long: "Reply in an existing discussion on a doc. --parent is the discussion's first comment, " +
		"or any reply in it (the reply joins the same discussion).\n\n" +
		"Pass the reply with --content, --content-file or --content-stdin; prefer --content-file for anything " +
		"longer than a line. @mentions use the mention links from the doc's discussion, e.g. " +
		"`[@Name](mention://member/<user-id>)`.\n\n" +
		"Starting a discussion, resolving one and deleting comments are reserved for people.",
	Args: exactArgs(1),
	RunE: runDocCommentAdd,
}

var docCommentListCmd = &cobra.Command{
	Use:   "list <doc-id>",
	Short: "List the comment threads on a doc",
	Long: "List the comment threads on a doc, each root comment followed by its replies. " +
		"A root comment's QUOTE (anchor_quote in --output json) is the doc text it was left on.\n\n" +
		"Pass --thread <comment-id> to list one discussion only. " +
		"Resolved threads are hidden unless you pass --include-resolved or --thread. " +
		"The table shortens long comments; --output json has the full text.\n\n" +
		"Reply in a thread with `multica doc comment add`. Starting a discussion, resolving one and " +
		"deleting comments are reserved for people.",
	Args: exactArgs(1),
	RunE: runDocCommentList,
}

var docFolderCmd = &cobra.Command{
	Use:   "folder",
	Short: "Manage knowledge base doc folders",
}

var docFolderListCmd = &cobra.Command{
	Use:   "list",
	Short: "List doc folders",
	RunE:  runDocFolderList,
}

var docFolderCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a doc folder",
	RunE:  runDocFolderCreate,
}

var docFolderUpdateCmd = &cobra.Command{
	Use:     "update <folder-id>",
	Aliases: []string{"rename"},
	Short:   "Rename a doc folder",
	Long: "Rename a folder in the workspace knowledge base.\n\n" +
		"<folder-id> accepts a full folder id or a unique prefix from `multica doc list` or `multica doc folder list`.",
	Args: exactArgs(1),
	RunE: runDocFolderUpdate,
}

var docFolderMoveCmd = &cobra.Command{
	Use:   "move <folder-id>",
	Short: "Move a doc folder into another folder, or to the top level",
	Long: "Move a folder into another folder of the workspace knowledge base.\n\n" +
		"<folder-id> and --parent accept a full folder id or a unique prefix from `multica doc list` or `multica doc folder list`. " +
		"Pass `--parent root` to move the folder to the top level.",
	Args: exactArgs(1),
	RunE: runDocFolderMove,
}

func init() {
	docCmd.AddCommand(docListCmd, docSearchCmd, docGetCmd, docCreateCmd, docUpdateCmd, docEditCmd, docMoveCmd,
		docHistoryCmd, docRestoreCmd, docCommentCmd, docFolderCmd)
	docFolderCmd.AddCommand(docFolderListCmd, docFolderCreateCmd, docFolderUpdateCmd, docFolderMoveCmd)
	docCommentCmd.AddCommand(docCommentListCmd, docCommentAddCmd)
	docCommentAddCmd.Flags().String("parent", "", "Comment id in the discussion to reply in (required)")
	docCommentAddCmd.Flags().String("content", "", "Reply in Markdown")
	docCommentAddCmd.Flags().String("content-file", "", "Read the reply from a UTF-8 file")
	docCommentAddCmd.Flags().Bool("content-stdin", false, "Read the reply from stdin")
	docCommentAddCmd.Flags().String("output", "json", "Output format: table or json")
	docCommentListCmd.Flags().String("thread", "", "Only this discussion: its first comment's id")
	docHistoryCmd.Flags().Int("version", 0, "Show this version with its content instead of listing versions")
	docHistoryCmd.Flags().String("output", "", "Output format: table or json for the list (default table); json or content with --version (default json)")
	docRestoreCmd.Flags().Int("version", 0, "Version number from doc history to restore (required)")
	docRestoreCmd.Flags().Int64("base-revision", 0, "Revision from doc get (required)")
	docRestoreCmd.Flags().String("message", "", "Change summary for history")
	docRestoreCmd.Flags().String("output", "json", "Output format: table or json")
	docCommentListCmd.Flags().Bool("include-resolved", false, "Include resolved threads")
	docCommentListCmd.Flags().String("output", "table", "Output format: table or json")
	docMoveCmd.Flags().String("folder", "", "Destination folder id or unique prefix, or \"root\" for the top level (required)")
	docMoveCmd.Flags().String("output", "json", "Output format: table or json")
	docListCmd.Flags().String("output", "table", "Output format: table or json")
	docSearchCmd.Flags().Int("limit", 20, "Maximum results (at most 50)")
	docSearchCmd.Flags().String("output", "table", "Output format: table or json")
	docGetCmd.Flags().String("output", "json", "Output format: json or content")
	for _, cmd := range []*cobra.Command{docCreateCmd, docUpdateCmd} {
		cmd.Flags().String("title", "", "Document title")
		cmd.Flags().String("message", "", "Change summary for history")
		cmd.Flags().String("content", "", "Markdown content")
		cmd.Flags().String("content-file", "", "Read Markdown from a UTF-8 file")
		cmd.Flags().Bool("content-stdin", false, "Read Markdown from stdin")
		cmd.Flags().String("output", "json", "Output format: table or json")
	}
	docCreateCmd.Flags().String("folder", "", "Folder id (default: root)")
	docUpdateCmd.Flags().Int64("base-revision", 0, "Revision from doc get (required)")
	docEditCmd.Flags().String("old", "", "Exact text to replace (this or --old-file is required)")
	docEditCmd.Flags().String("old-file", "", "Read the text to replace from a UTF-8 file")
	docEditCmd.Flags().String("new", "", "Replacement text; \"\" deletes it (this or --new-file is required)")
	docEditCmd.Flags().String("new-file", "", "Read the replacement from a UTF-8 file")
	docEditCmd.Flags().Bool("replace-all", false, "Replace every occurrence of --old")
	docEditCmd.Flags().String("message", "", "Change summary for history")
	docEditCmd.Flags().String("output", "json", "Output format: table or json")
	docFolderListCmd.Flags().String("output", "table", "Output format: table or json")
	docFolderCreateCmd.Flags().String("title", "", "Folder title (required)")
	docFolderCreateCmd.Flags().String("parent", "", "Parent folder id or unique prefix, or \"root\" for the top level (default: root)")
	docFolderCreateCmd.Flags().String("output", "json", "Output format: table or json")
	docFolderUpdateCmd.Flags().String("title", "", "Folder title (required)")
	docFolderUpdateCmd.Flags().String("output", "json", "Output format: table or json")
	docFolderMoveCmd.Flags().String("parent", "", "Destination parent folder id or unique prefix, or \"root\" for the top level (required)")
	docFolderMoveCmd.Flags().String("output", "json", "Output format: table or json")
}

type docTree struct {
	Folders []map[string]any `json:"folders"`
	Docs    []map[string]any `json:"docs"`
}

func fetchDocTree(ctx context.Context, client *cli.APIClient) (docTree, error) {
	var tree docTree
	if err := client.GetJSON(ctx, "/api/docs", &tree); err != nil {
		return tree, fmt.Errorf("list docs: %w", err)
	}
	return tree, nil
}

// resolveDocRef accepts a full id or an unambiguous id prefix from `doc list`.
func resolveDocRef(ctx context.Context, client *cli.APIClient, ref string, folders bool) (string, error) {
	kind, listHint := "doc", "run `multica doc list`"
	if folders {
		kind, listHint = "folder", "run `multica doc folder list`"
	}
	resolved, err := resolveIDByPrefixWithListHint(ctx, client, kind, ref, listHint, func(ctx context.Context, client *cli.APIClient) ([]idCandidate, error) {
		tree, err := fetchDocTree(ctx, client)
		if err != nil {
			return nil, err
		}
		items := tree.Docs
		if folders {
			items = tree.Folders
		}
		candidates := make([]idCandidate, 0, len(items))
		for _, item := range items {
			candidates = append(candidates, idCandidate{ID: strVal(item, "id"), Display: strVal(item, "title")})
		}
		return candidates, nil
	})
	if err != nil {
		return "", err
	}
	return resolved.ID, nil
}

func resolveDocFolderTarget(ctx context.Context, client *cli.APIClient, ref string) (any, error) {
	ref = strings.TrimSpace(ref)
	if ref == "root" || ref == "" {
		return nil, nil
	}
	return resolveDocRef(ctx, client, ref, true)
}

// docWriteError turns a stale-revision 409 into instructions an agent can act on.
func docWriteError(action string, base int64, err error) error {
	var httpErr *cli.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusConflict {
		switch {
		case action == "restore doc" && strings.Contains(httpErr.Body, "revision_conflict"):
			return fmt.Errorf("%s: the doc changed since revision %d, so nothing was restored. "+
				"A restore replaces the whole doc, including those newer edits: run `multica doc get` to read them, "+
				"and retry with the new --base-revision only if the whole doc should still go back to that version (%s)",
				action, base, httpErr.Body)
		case strings.Contains(httpErr.Body, "revision_conflict"):
			return fmt.Errorf("%s: the doc changed since revision %d, so nothing was written. "+
				"Run `multica doc get` for the latest content and revision, merge your change into it, and retry with the new --base-revision (%s)",
				action, base, httpErr.Body)
		case strings.Contains(httpErr.Body, "doc_merge_conflict"):
			return fmt.Errorf("%s: someone changed the same paragraphs since revision %d, so nothing was written. "+
				"The conflicts list both versions; run `multica doc get` and redo your change on that content, "+
				"with `multica doc edit`, or `multica doc update` with the new --base-revision. "+
				"Do not resend the same body — it would erase their edits (%s)", action, base, httpErr.Body)
		case strings.Contains(httpErr.Body, "doc_edit_failed"):
			return fmt.Errorf("%s: nothing was written because --old does not match the doc exactly once. "+
				"Run `multica doc get --output content`, quote the current text (with more context if it repeats), and retry (%s)",
				action, httpErr.Body)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}

func docFolderPaths(folders []map[string]any) map[string]string {
	byID := make(map[string]map[string]any, len(folders))
	for _, f := range folders {
		byID[strVal(f, "id")] = f
	}
	paths := make(map[string]string, len(folders))
	var pathOf func(id string, depth int) string
	pathOf = func(id string, depth int) string {
		if p, ok := paths[id]; ok {
			return p
		}
		f, ok := byID[id]
		if !ok || depth > len(folders) {
			return ""
		}
		p := strVal(f, "title")
		if parent := strVal(f, "parent_id"); parent != "" {
			if pp := pathOf(parent, depth+1); pp != "" {
				p = pp + "/" + p
			}
		}
		paths[id] = p
		return p
	}
	for id := range byID {
		pathOf(id, 0)
	}
	return paths
}

func printDocSummary(doc map[string]any, output string) error {
	if output == "table" {
		cli.PrintTable(os.Stdout, []string{"ID", "TITLE", "REVISION", "VERSIONS"}, [][]string{{
			strVal(doc, "id"), strVal(doc, "title"),
			strVal(doc, "revision"), strVal(doc, "version_count"),
		}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, doc)
}

func runDocList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	tree, err := fetchDocTree(ctx, client)
	if err != nil {
		return err
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, tree)
	}

	paths := docFolderPaths(tree.Folders)
	rows := make([][]string, 0, len(tree.Folders)+len(tree.Docs))
	for _, f := range tree.Folders {
		rows = append(rows, []string{strVal(f, "id"), "folder", paths[strVal(f, "id")] + "/", "", ""})
	}
	for _, d := range tree.Docs {
		path := strVal(d, "title")
		if folder := paths[strVal(d, "folder_id")]; folder != "" {
			path = folder + "/" + path
		}
		updated := strVal(d, "updated_at")
		if len(updated) >= 10 {
			updated = updated[:10]
		}
		rows = append(rows, []string{strVal(d, "id"), "doc", path, strVal(d, "revision"), updated})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i][2] < rows[j][2] })
	cli.PrintTable(os.Stdout, []string{"ID", "TYPE", "PATH", "REVISION", "UPDATED"}, rows)
	return nil
}

func runDocSearch(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	params := url.Values{}
	params.Set("q", args[0])
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		params.Set("limit", fmt.Sprintf("%d", v))
	}
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/docs/search?"+params.Encode(), &result); err != nil {
		return fmt.Errorf("search docs: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	docs, _ := result["docs"].([]any)
	rows := make([][]string, 0, len(docs))
	for _, raw := range docs {
		doc, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		match := strVal(doc, "match_source")
		if snippet := strVal(doc, "matched_snippet"); snippet != "" {
			if runes := []rune(snippet); len(runes) > 60 {
				snippet = string(runes[:57]) + "..."
			}
			match += ": " + snippet
		}
		updated := strVal(doc, "updated_at")
		if len(updated) >= 10 {
			updated = updated[:10]
		}
		rows = append(rows, []string{strVal(doc, "id"), strVal(doc, "title"), updated, match})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "TITLE", "UPDATED", "MATCH"}, rows)
	return nil
}

func runDocMove(cmd *cobra.Command, args []string) error {
	destination, _ := cmd.Flags().GetString("folder")
	if strings.TrimSpace(destination) == "" {
		return fmt.Errorf("--folder is required: a folder id, or `root` for the top level")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	id, err := resolveDocRef(ctx, client, args[0], false)
	if err != nil {
		return err
	}
	folder, err := resolveDocFolderTarget(ctx, client, destination)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := client.PostJSON(ctx, "/api/docs/"+id+"/move", map[string]any{"folder_id": folder}, &doc); err != nil {
		return fmt.Errorf("move doc: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	return printDocSummary(doc, output)
}

func runDocGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	id, err := resolveDocRef(ctx, client, args[0], false)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := client.GetJSON(ctx, "/api/docs/"+id+"?flush=true", &doc); err != nil {
		return fmt.Errorf("get doc: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "content" {
		_, err := fmt.Fprint(os.Stdout, strVal(doc, "content"))
		return err
	}
	return cli.PrintJSON(os.Stdout, doc)
}

func runDocCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("--title is required")
	}
	content, _, err := resolveSkillContentFlag(cmd)
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"title": title, "content": content}
	if v, _ := cmd.Flags().GetString("message"); v != "" {
		body["message"] = v
	}
	if v, _ := cmd.Flags().GetString("folder"); v != "" {
		folder, err := resolveDocFolderTarget(ctx, client, v)
		if err != nil {
			return err
		}
		body["folder_id"] = folder
	}
	var doc map[string]any
	if err := client.PostJSON(ctx, "/api/docs", body, &doc); err != nil {
		return fmt.Errorf("create doc: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	return printDocSummary(doc, output)
}

func runDocUpdate(cmd *cobra.Command, args []string) error {
	base, _ := cmd.Flags().GetInt64("base-revision")
	if base <= 0 {
		return fmt.Errorf("--base-revision is required: pass the revision from `multica doc get`")
	}
	content, hasContent, err := resolveSkillContentFlag(cmd)
	if err != nil {
		return err
	}
	body := map[string]any{"base_revision": base}
	if hasContent {
		body["content"] = content
	}
	if cmd.Flags().Changed("title") {
		title, _ := cmd.Flags().GetString("title")
		body["title"] = title
	}
	if !hasContent && !cmd.Flags().Changed("title") {
		return fmt.Errorf("nothing to update: pass --title and/or --content, --content-file, --content-stdin")
	}
	if v, _ := cmd.Flags().GetString("message"); v != "" {
		body["message"] = v
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	id, err := resolveDocRef(ctx, client, args[0], false)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := client.PatchJSON(ctx, "/api/docs/"+id, body, &doc); err != nil {
		return docWriteError("update doc", base, err)
	}
	delete(doc, "content")
	output, _ := cmd.Flags().GetString("output")
	return printDocSummary(doc, output)
}

func readEditText(cmd *cobra.Command, flag string) (string, bool, error) {
	if path, _ := cmd.Flags().GetString(flag + "-file"); path != "" {
		if cmd.Flags().Changed(flag) {
			return "", false, fmt.Errorf("pass only one of --%s and --%s-file", flag, flag)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", false, fmt.Errorf("read --%s-file: %w", flag, err)
		}
		return string(data), true, nil
	}
	v, _ := cmd.Flags().GetString(flag)
	return v, cmd.Flags().Changed(flag), nil
}

func runDocEdit(cmd *cobra.Command, args []string) error {
	oldText, hasOld, err := readEditText(cmd, "old")
	if err != nil {
		return err
	}
	if !hasOld || oldText == "" {
		return fmt.Errorf("--old (or --old-file) is required: the exact text to replace")
	}
	newText, hasNew, err := readEditText(cmd, "new")
	if err != nil {
		return err
	}
	if !hasNew {
		return fmt.Errorf("--new (or --new-file) is required; pass --new \"\" to delete the text")
	}
	replaceAll, _ := cmd.Flags().GetBool("replace-all")
	body := map[string]any{
		"edits": []map[string]any{{"old_text": oldText, "new_text": newText, "replace_all": replaceAll}},
	}
	if v, _ := cmd.Flags().GetString("message"); v != "" {
		body["message"] = v
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	id, err := resolveDocRef(ctx, client, args[0], false)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := client.PostJSON(ctx, "/api/docs/"+id+"/edits", body, &doc); err != nil {
		return docWriteError("edit doc", 0, err)
	}
	delete(doc, "content")
	output, _ := cmd.Flags().GetString("output")
	return printDocSummary(doc, output)
}

func docActor(m map[string]any, typeKey, idKey string) string {
	if strVal(m, idKey) == "" {
		return ""
	}
	return strVal(m, typeKey) + ":" + truncateID(strVal(m, idKey))
}

func runDocHistory(cmd *cobra.Command, args []string) error {
	number, _ := cmd.Flags().GetInt("version")
	output, _ := cmd.Flags().GetString("output")
	if cmd.Flags().Changed("version") && number <= 0 {
		return fmt.Errorf("--version must be a version number from `multica doc history`")
	}
	switch {
	case number > 0 && output != "" && output != "json" && output != "content":
		return fmt.Errorf("--output must be json or content with --version")
	case number == 0 && output != "" && output != "table" && output != "json":
		return fmt.Errorf("--output must be table or json; pass --version to see a version's content")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	id, err := resolveDocRef(ctx, client, args[0], false)
	if err != nil {
		return err
	}

	if number > 0 {
		var version map[string]any
		if err := client.GetJSON(ctx, fmt.Sprintf("/api/docs/%s/versions/%d", id, number), &version); err != nil {
			return fmt.Errorf("get doc version %d: %w", number, err)
		}
		if output == "content" {
			_, err := fmt.Fprint(os.Stdout, strVal(version, "content"))
			return err
		}
		return cli.PrintJSON(os.Stdout, version)
	}

	var result struct {
		Versions []map[string]any `json:"versions"`
	}
	if err := client.GetJSON(ctx, "/api/docs/"+id+"/versions", &result); err != nil {
		return fmt.Errorf("list doc versions: %w", err)
	}
	if result.Versions == nil {
		result.Versions = []map[string]any{}
	}
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result.Versions)
	}
	rows := make([][]string, 0, len(result.Versions))
	for _, v := range result.Versions {
		action := strVal(v, "action")
		if ref := strVal(v, "action_ref"); action == "restore" && ref != "" {
			action = "restore of v" + ref
		}
		saved := strVal(v, "updated_at")
		if len(saved) >= 16 {
			saved = strings.Replace(saved[:16], "T", " ", 1)
		}
		rows = append(rows, []string{
			strVal(v, "number"), saved, docActor(v, "author_type", "author_id"),
			action, oneLine(strVal(v, "message"), 60),
		})
	}
	cli.PrintTable(os.Stdout, []string{"VERSION", "SAVED", "AUTHOR", "ACTION", "MESSAGE"}, rows)
	return nil
}

func runDocRestore(cmd *cobra.Command, args []string) error {
	number, _ := cmd.Flags().GetInt("version")
	if number <= 0 {
		return fmt.Errorf("--version is required: a version number from `multica doc history`")
	}
	base, _ := cmd.Flags().GetInt64("base-revision")
	if base <= 0 {
		return fmt.Errorf("--base-revision is required: pass the revision from `multica doc get`")
	}
	body := map[string]any{"base_revision": base, "action": "restore", "action_ref": number}
	if v, _ := cmd.Flags().GetString("message"); v != "" {
		body["message"] = v
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	id, err := resolveDocRef(ctx, client, args[0], false)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := client.PatchJSON(ctx, "/api/docs/"+id, body, &doc); err != nil {
		return docWriteError("restore doc", base, err)
	}
	delete(doc, "content")
	output, _ := cmd.Flags().GetString("output")
	return printDocSummary(doc, output)
}

func runDocCommentList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	id, err := resolveDocRef(ctx, client, args[0], false)
	if err != nil {
		return err
	}
	var result struct {
		Comments []map[string]any `json:"comments"`
	}
	if err := client.GetJSON(ctx, "/api/docs/"+id+"/comments", &result); err != nil {
		return fmt.Errorf("list doc comments: %w", err)
	}

	// Group replies under their root, keeping the server's chronological order.
	includeResolved, _ := cmd.Flags().GetBool("include-resolved")
	thread, _ := cmd.Flags().GetString("thread")
	var roots []map[string]any
	replies := map[string][]map[string]any{}
	for _, c := range result.Comments {
		if parent := strVal(c, "parent_id"); parent != "" {
			replies[parent] = append(replies[parent], c)
			continue
		}
		if thread != "" {
			if strVal(c, "id") == thread {
				roots = append(roots, c)
			}
		} else if includeResolved || strVal(c, "resolved_at") == "" {
			roots = append(roots, c)
		}
	}
	if thread != "" && len(roots) == 0 {
		return fmt.Errorf("discussion %s not found on this doc", thread)
	}
	threads := make([]map[string]any, 0, len(result.Comments))
	for _, root := range roots {
		threads = append(threads, root)
		threads = append(threads, replies[strVal(root, "id")]...)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, threads)
	}
	rows := make([][]string, 0, len(threads))
	for _, c := range threads {
		replyTo, status, quote := strVal(c, "parent_id"), "", ""
		if replyTo == "" {
			replyTo, status, quote = "-", "open", oneLine(strVal(c, "anchor_quote"), 40)
			if strVal(c, "resolved_at") != "" {
				status = "resolved"
			}
		}
		rows = append(rows, []string{
			strVal(c, "id"), replyTo, docActor(c, "author_type", "author_id"),
			status, quote, oneLine(strVal(c, "content"), 80),
		})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "REPLY TO", "AUTHOR", "STATUS", "QUOTE", "COMMENT"}, rows)
	return nil
}

func runDocCommentAdd(cmd *cobra.Command, args []string) error {
	parent, _ := cmd.Flags().GetString("parent")
	if strings.TrimSpace(parent) == "" {
		return fmt.Errorf("--parent is required: replies go in an existing discussion")
	}
	content, hasContent, err := resolveSkillContentFlag(cmd)
	if err != nil {
		return err
	}
	if !hasContent || strings.TrimSpace(content) == "" {
		return fmt.Errorf("reply is empty: pass --content, --content-file or --content-stdin")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	id, err := resolveDocRef(ctx, client, args[0], false)
	if err != nil {
		return err
	}
	var comment map[string]any
	body := map[string]any{"content": content, "parent_id": parent}
	if err := client.PostJSON(ctx, "/api/docs/"+id+"/comments", body, &comment); err != nil {
		return fmt.Errorf("reply in doc discussion: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "table" {
		cli.PrintTable(os.Stdout, []string{"ID", "REPLY TO", "COMMENT"}, [][]string{{
			strVal(comment, "id"), strVal(comment, "parent_id"), oneLine(strVal(comment, "content"), 80),
		}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, comment)
}

func printDocFolderSummary(folder map[string]any, output string) error {
	if output == "table" {
		parent := strVal(folder, "parent_id")
		if parent == "" {
			parent = "root"
		}
		cli.PrintTable(os.Stdout, []string{"ID", "TITLE", "PARENT"}, [][]string{{
			strVal(folder, "id"), strVal(folder, "title"), parent,
		}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, folder)
}

func runDocFolderList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	tree, err := fetchDocTree(ctx, client)
	if err != nil {
		return err
	}
	folders := tree.Folders
	if folders == nil {
		folders = []map[string]any{}
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, folders)
	}

	paths := docFolderPaths(folders)
	rows := make([][]string, 0, len(folders))
	for _, f := range folders {
		id := strVal(f, "id")
		updated := strVal(f, "updated_at")
		if len(updated) >= 10 {
			updated = updated[:10]
		}
		rows = append(rows, []string{id, paths[id] + "/", updated})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i][1] < rows[j][1] })
	cli.PrintTable(os.Stdout, []string{"ID", "PATH", "UPDATED"}, rows)
	return nil
}

func runDocFolderCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("--title is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"title": title}
	if v, _ := cmd.Flags().GetString("parent"); strings.TrimSpace(v) != "" {
		parent, err := resolveDocFolderTarget(ctx, client, v)
		if err != nil {
			return err
		}
		body["parent_id"] = parent
	}
	var folder map[string]any
	if err := client.PostJSON(ctx, "/api/doc-folders", body, &folder); err != nil {
		return fmt.Errorf("create doc folder: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	return printDocFolderSummary(folder, output)
}

func runDocFolderUpdate(cmd *cobra.Command, args []string) error {
	title, _ := cmd.Flags().GetString("title")
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("--title is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	id, err := resolveDocRef(ctx, client, args[0], true)
	if err != nil {
		return err
	}
	var folder map[string]any
	if err := client.PatchJSON(ctx, "/api/doc-folders/"+id, map[string]any{"title": title}, &folder); err != nil {
		return fmt.Errorf("update doc folder: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	return printDocFolderSummary(folder, output)
}

func runDocFolderMove(cmd *cobra.Command, args []string) error {
	destination, _ := cmd.Flags().GetString("parent")
	if strings.TrimSpace(destination) == "" {
		return fmt.Errorf("--parent is required: a folder id, or `root` for the top level")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	id, err := resolveDocRef(ctx, client, args[0], true)
	if err != nil {
		return err
	}
	parent, err := resolveDocFolderTarget(ctx, client, destination)
	if err != nil {
		return err
	}
	var folder map[string]any
	if err := client.PostJSON(ctx, "/api/doc-folders/"+id+"/move", map[string]any{"parent_id": parent}, &folder); err != nil {
		return fmt.Errorf("move doc folder: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	return printDocFolderSummary(folder, output)
}
