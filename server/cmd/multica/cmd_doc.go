package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
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
		"Pass --message to say what changed and why — it shows in the doc's history.",
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

func init() {
	docCmd.AddCommand(docListCmd, docGetCmd, docCreateCmd, docUpdateCmd)
	docListCmd.Flags().String("output", "table", "Output format: table or json")
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
