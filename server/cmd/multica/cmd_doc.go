package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Doc commands — the workspace knowledge base (MUL-8370).
//
// Writes are optimistic: `doc update` must name the revision it edited
// (`--base-revision`, from `doc get`). When someone else saved in between the
// server answers 409 and nothing is written; re-read, merge, retry.

var docCmd = &cobra.Command{
	Use:   "doc",
	Short: "Work with knowledge base docs",
	Long: "Read and write docs in the workspace knowledge base.\n\n" +
		"Every update needs --base-revision: the revision you read with `multica doc get`. " +
		"If the doc changed since then the update is refused; re-read it, merge your change, and retry. " +
		"Pass --message to say what changed and why — it shows in the doc's history and the recent-updates feed.",
}

var docListCmd = &cobra.Command{
	Use:   "list",
	Short: "List docs and folders",
	RunE:  runDocList,
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
	Args:  exactArgs(1),
	RunE:  runDocUpdate,
}

var docHistoryCmd = &cobra.Command{
	Use:   "history <doc-id>",
	Short: "List a doc's versions",
	Args:  exactArgs(1),
	RunE:  runDocHistory,
}

var docVersionCmd = &cobra.Command{
	Use:   "version <doc-id> <number>",
	Short: "Show one version of a doc",
	Args:  exactArgs(2),
	RunE:  runDocVersion,
}

var docRestoreCmd = &cobra.Command{
	Use:   "restore <doc-id> <number>",
	Short: "Restore a doc to an earlier version (recorded as a new version)",
	Args:  exactArgs(2),
	RunE:  runDocRestore,
}

var docMoveCmd = &cobra.Command{
	Use:   "move <doc-id>",
	Short: "Move a doc into a folder (or to the root with --folder root)",
	Args:  exactArgs(1),
	RunE:  runDocMove,
}

var docDeleteCmd = &cobra.Command{
	Use:   "delete <doc-id>",
	Short: "Delete a doc and its history",
	Args:  exactArgs(1),
	RunE:  runDocDelete,
}

var docLinkCmd = &cobra.Command{
	Use:   "link <doc-id>",
	Short: "Link a doc to a project",
	Args:  exactArgs(1),
	RunE:  runDocLink,
}

var docUnlinkCmd = &cobra.Command{
	Use:   "unlink <doc-id>",
	Short: "Unlink a doc from a project",
	Args:  exactArgs(1),
	RunE:  runDocUnlink,
}

var docFolderCmd = &cobra.Command{
	Use:   "folder",
	Short: "Work with knowledge base folders",
}

var docFolderCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a folder",
	RunE:  runDocFolderCreate,
}

var docFolderRenameCmd = &cobra.Command{
	Use:   "rename <folder-id>",
	Short: "Rename a folder",
	Args:  exactArgs(1),
	RunE:  runDocFolderRename,
}

var docFolderMoveCmd = &cobra.Command{
	Use:   "move <folder-id>",
	Short: "Move a folder under another folder (or to the root with --parent root)",
	Args:  exactArgs(1),
	RunE:  runDocFolderMove,
}

var docFolderDeleteCmd = &cobra.Command{
	Use:   "delete <folder-id>",
	Short: "Delete an empty folder",
	Args:  exactArgs(1),
	RunE:  runDocFolderDelete,
}

func init() {
	docCmd.AddCommand(docListCmd, docGetCmd, docCreateCmd, docUpdateCmd, docHistoryCmd, docVersionCmd,
		docRestoreCmd, docMoveCmd, docDeleteCmd, docLinkCmd, docUnlinkCmd, docFolderCmd)
	docFolderCmd.AddCommand(docFolderCreateCmd, docFolderRenameCmd, docFolderMoveCmd, docFolderDeleteCmd)

	docListCmd.Flags().String("output", "table", "Output format: table or json")
	docGetCmd.Flags().String("output", "json", "Output format: json, or content to print only the body")

	docCreateCmd.Flags().String("title", "", "Doc title (required)")
	docCreateCmd.Flags().String("format", "markdown", "Doc format: markdown or html")
	docCreateCmd.Flags().String("folder", "", "Folder id to create the doc in (default: root)")
	docCreateCmd.Flags().String("message", "", "What this doc is for; shown in history")
	docCreateCmd.Flags().String("content", "", "Doc content")
	docCreateCmd.Flags().Bool("content-stdin", false, "Read content from stdin. Mutually exclusive with --content and --content-file.")
	docCreateCmd.Flags().String("content-file", "", "Read content from a UTF-8 file. Mutually exclusive with --content and --content-stdin.")
	docCreateCmd.Flags().String("output", "json", "Output format: table or json")

	docUpdateCmd.Flags().Int64("base-revision", 0, "Revision you edited, from `multica doc get` (required)")
	docUpdateCmd.Flags().String("title", "", "New title")
	docUpdateCmd.Flags().String("message", "", "What changed and why; shown in history and the recent-updates feed")
	docUpdateCmd.Flags().String("content", "", "New content (replaces the whole body)")
	docUpdateCmd.Flags().Bool("content-stdin", false, "Read new content from stdin. Mutually exclusive with --content and --content-file.")
	docUpdateCmd.Flags().String("content-file", "", "Read new content from a UTF-8 file. Mutually exclusive with --content and --content-stdin.")
	docUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	docHistoryCmd.Flags().String("output", "table", "Output format: table or json")
	docVersionCmd.Flags().String("output", "json", "Output format: json, or content to print only the body")

	docRestoreCmd.Flags().Int64("base-revision", 0, "Current revision of the doc, from `multica doc get` (required)")
	docRestoreCmd.Flags().String("output", "json", "Output format: table or json")

	docMoveCmd.Flags().String("folder", "", "Target folder id, or root (required)")
	docMoveCmd.Flags().String("output", "json", "Output format: table or json")

	docLinkCmd.Flags().String("project", "", "Project id (required)")
	docUnlinkCmd.Flags().String("project", "", "Project id (required)")

	docFolderCreateCmd.Flags().String("title", "", "Folder name (required)")
	docFolderCreateCmd.Flags().String("parent", "", "Parent folder id (default: root)")
	docFolderCreateCmd.Flags().String("output", "json", "Output format: table or json")
	docFolderRenameCmd.Flags().String("title", "", "New folder name (required)")
	docFolderRenameCmd.Flags().String("output", "json", "Output format: table or json")
	docFolderMoveCmd.Flags().String("parent", "", "Target parent folder id, or root (required)")
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
	if len(ref) == 36 {
		return ref, nil
	}
	tree, err := fetchDocTree(ctx, client)
	if err != nil {
		return "", err
	}
	items := tree.Docs
	kind := "doc"
	if folders {
		items = tree.Folders
		kind = "folder"
	}
	var matches []string
	for _, item := range items {
		if id := strVal(item, "id"); strings.HasPrefix(id, ref) {
			matches = append(matches, id)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no %s matches %q", kind, ref)
	default:
		return "", fmt.Errorf("%q matches %d %ss; use a longer id", ref, len(matches), kind)
	}
}

func resolveDocFolderTarget(ctx context.Context, client *cli.APIClient, ref string) (any, error) {
	if ref == "root" || ref == "" {
		return nil, nil
	}
	return resolveDocRef(ctx, client, ref, true)
}

// docWriteError turns a stale-revision 409 into instructions an agent can act on.
func docWriteError(action string, base int64, err error) error {
	var httpErr *cli.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusConflict && strings.Contains(httpErr.Body, "revision_conflict") {
		return fmt.Errorf("%s: the doc changed since revision %d, so nothing was written. "+
			"Run `multica doc get` for the latest content and revision, merge your change into it, and retry with the new --base-revision (%s)",
			action, base, httpErr.Body)
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
		cli.PrintTable(os.Stdout, []string{"ID", "TITLE", "FORMAT", "REVISION", "VERSIONS"}, [][]string{{
			strVal(doc, "id"), strVal(doc, "title"), strVal(doc, "format"),
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
		rows = append(rows, []string{strVal(d, "id"), strVal(d, "format"), path, strVal(d, "revision"), updated})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i][2] < rows[j][2] })
	cli.PrintTable(os.Stdout, []string{"ID", "TYPE", "PATH", "REVISION", "UPDATED"}, rows)
	return nil
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
	if err := client.GetJSON(ctx, "/api/docs/"+id, &doc); err != nil {
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
	format, _ := cmd.Flags().GetString("format")
	if format != "markdown" && format != "html" {
		return fmt.Errorf("--format must be markdown or html")
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

	body := map[string]any{"title": title, "format": format, "content": content}
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

func runDocHistory(cmd *cobra.Command, args []string) error {
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
		Versions []map[string]any `json:"versions"`
	}
	if err := client.GetJSON(ctx, "/api/docs/"+id+"/versions", &result); err != nil {
		return fmt.Errorf("list versions: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result.Versions)
	}
	rows := make([][]string, 0, len(result.Versions))
	for _, v := range result.Versions {
		action := strVal(v, "action")
		if ref := strVal(v, "action_ref"); ref != "" {
			action += " v" + ref
		}
		rows = append(rows, []string{
			"v" + strVal(v, "number"),
			strVal(v, "author_type") + ":" + displayID(strVal(v, "author_id"), false),
			action,
			"+" + strVal(v, "lines_added") + " -" + strVal(v, "lines_removed"),
			strVal(v, "updated_at"),
			strVal(v, "message"),
		})
	}
	cli.PrintTable(os.Stdout, []string{"VERSION", "AUTHOR", "ACTION", "LINES", "UPDATED", "MESSAGE"}, rows)
	return nil
}

func runDocVersion(cmd *cobra.Command, args []string) error {
	number, err := strconv.Atoi(strings.TrimPrefix(args[1], "v"))
	if err != nil || number <= 0 {
		return fmt.Errorf("version must be a positive number")
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
	var version map[string]any
	if err := client.GetJSON(ctx, fmt.Sprintf("/api/docs/%s/versions/%d", id, number), &version); err != nil {
		return fmt.Errorf("get version: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "content" {
		_, err := fmt.Fprint(os.Stdout, strVal(version, "content"))
		return err
	}
	return cli.PrintJSON(os.Stdout, version)
}

func runDocRestore(cmd *cobra.Command, args []string) error {
	base, _ := cmd.Flags().GetInt64("base-revision")
	if base <= 0 {
		return fmt.Errorf("--base-revision is required: pass the revision from `multica doc get`")
	}
	number, err := strconv.Atoi(strings.TrimPrefix(args[1], "v"))
	if err != nil || number <= 0 {
		return fmt.Errorf("version must be a positive number")
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
	body := map[string]any{"base_revision": base, "action": "restore", "action_ref": number}
	if err := client.PatchJSON(ctx, "/api/docs/"+id, body, &doc); err != nil {
		return docWriteError("restore doc", base, err)
	}
	delete(doc, "content")
	output, _ := cmd.Flags().GetString("output")
	return printDocSummary(doc, output)
}

func runDocMove(cmd *cobra.Command, args []string) error {
	target, _ := cmd.Flags().GetString("folder")
	if target == "" {
		return fmt.Errorf("--folder is required (a folder id, or root)")
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
	folder, err := resolveDocFolderTarget(ctx, client, target)
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

func runDocDelete(cmd *cobra.Command, args []string) error {
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
	if err := client.DeleteJSON(ctx, "/api/docs/"+id); err != nil {
		return fmt.Errorf("delete doc: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Deleted doc %s\n", id)
	return nil
}

func runDocLink(cmd *cobra.Command, args []string) error {
	project, _ := cmd.Flags().GetString("project")
	if project == "" {
		return fmt.Errorf("--project is required")
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
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/projects/"+project+"/docs", map[string]any{"doc_id": id}, &result); err != nil {
		return fmt.Errorf("link doc: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Linked doc %s to project %s\n", id, project)
	return nil
}

func runDocUnlink(cmd *cobra.Command, args []string) error {
	project, _ := cmd.Flags().GetString("project")
	if project == "" {
		return fmt.Errorf("--project is required")
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
	if err := client.DeleteJSON(ctx, "/api/projects/"+project+"/docs/"+id); err != nil {
		return fmt.Errorf("unlink doc: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Unlinked doc %s from project %s\n", id, project)
	return nil
}

func printDocFolder(folder map[string]any, output string) error {
	if output == "table" {
		cli.PrintTable(os.Stdout, []string{"ID", "TITLE", "PARENT"}, [][]string{{
			strVal(folder, "id"), strVal(folder, "title"), strVal(folder, "parent_id"),
		}})
		return nil
	}
	return cli.PrintJSON(os.Stdout, folder)
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
	if v, _ := cmd.Flags().GetString("parent"); v != "" {
		parent, err := resolveDocFolderTarget(ctx, client, v)
		if err != nil {
			return err
		}
		body["parent_id"] = parent
	}
	var folder map[string]any
	if err := client.PostJSON(ctx, "/api/doc-folders", body, &folder); err != nil {
		return fmt.Errorf("create folder: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	return printDocFolder(folder, output)
}

func runDocFolderRename(cmd *cobra.Command, args []string) error {
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
		return fmt.Errorf("rename folder: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	return printDocFolder(folder, output)
}

func runDocFolderMove(cmd *cobra.Command, args []string) error {
	target, _ := cmd.Flags().GetString("parent")
	if target == "" {
		return fmt.Errorf("--parent is required (a folder id, or root)")
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
	parent, err := resolveDocFolderTarget(ctx, client, target)
	if err != nil {
		return err
	}
	var folder map[string]any
	if err := client.PostJSON(ctx, "/api/doc-folders/"+id+"/move", map[string]any{"parent_id": parent}, &folder); err != nil {
		return fmt.Errorf("move folder: %w", err)
	}
	output, _ := cmd.Flags().GetString("output")
	return printDocFolder(folder, output)
}

func runDocFolderDelete(cmd *cobra.Command, args []string) error {
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
	if err := client.DeleteJSON(ctx, "/api/doc-folders/"+id); err != nil {
		return fmt.Errorf("delete folder: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Deleted folder %s\n", id)
	return nil
}
