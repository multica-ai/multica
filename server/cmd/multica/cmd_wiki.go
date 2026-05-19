package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var wikiCmd = &cobra.Command{
	Use:   "wiki",
	Short: "Work with wiki pages",
}

var wikiListCmd = &cobra.Command{
	Use:   "list",
	Short: "List wiki pages in the workspace",
	RunE:  runWikiList,
}

var wikiGetCmd = &cobra.Command{
	Use:   "get <page-id>",
	Short: "Get wiki page details",
	Args:  exactArgs(1),
	RunE:  runWikiGet,
}

var wikiCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new wiki page",
	RunE:  runWikiCreate,
}

var wikiUpdateCmd = &cobra.Command{
	Use:   "update <page-id>",
	Short: "Update a wiki page",
	Args:  exactArgs(1),
	RunE:  runWikiUpdate,
}

var wikiDeleteCmd = &cobra.Command{
	Use:   "delete <page-id>",
	Short: "Delete a wiki page",
	Args:  exactArgs(1),
	RunE:  runWikiDelete,
}

func init() {
	wikiCmd.AddCommand(wikiListCmd)
	wikiCmd.AddCommand(wikiGetCmd)
	wikiCmd.AddCommand(wikiCreateCmd)
	wikiCmd.AddCommand(wikiUpdateCmd)
	wikiCmd.AddCommand(wikiDeleteCmd)

	// wiki list
	wikiListCmd.Flags().String("output", "table", "Output format: table or json")
	wikiListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	// wiki get
	wikiGetCmd.Flags().String("output", "json", "Output format: table or json")

	// wiki create
	wikiCreateCmd.Flags().String("title", "", "Page title (required)")
	wikiCreateCmd.Flags().String("content", "", "Page content (inline)")
	wikiCreateCmd.Flags().Bool("content-stdin", false, "Read content from stdin")
	wikiCreateCmd.Flags().String("content-file", "", "Read content from a UTF-8 file")
	wikiCreateCmd.Flags().String("parent", "", "Parent page ID")
	wikiCreateCmd.Flags().Float64("position", 0, "Page position (0 = auto)")
	wikiCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// wiki update
	wikiUpdateCmd.Flags().String("title", "", "New title")
	wikiUpdateCmd.Flags().String("content", "", "New content (inline)")
	wikiUpdateCmd.Flags().Bool("content-stdin", false, "Read content from stdin")
	wikiUpdateCmd.Flags().String("content-file", "", "Read content from a UTF-8 file")
	wikiUpdateCmd.Flags().Float64("position", 0, "New position")
	wikiUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// wiki delete
	wikiDeleteCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Wiki commands
// ---------------------------------------------------------------------------

func runWikiList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}

	path := "/api/wiki-pages"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list wiki pages: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	pagesRaw, _ := result["pages"].([]any)
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "TITLE", "SLUG", "PARENT", "POSITION", "UPDATED"}
	rows := make([][]string, 0, len(pagesRaw))
	for _, raw := range pagesRaw {
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		parentID := strVal(p, "parent_id")
		if parentID == "" {
			parentID = "-"
		} else if !fullID {
			parentID = displayID(parentID, false)
		}
		updated := strVal(p, "updated_at")
		if len(updated) >= 10 {
			updated = updated[:10]
		}
		rows = append(rows, []string{
			displayID(strVal(p, "id"), fullID),
			strVal(p, "title"),
			strVal(p, "slug"),
			parentID,
			fmt.Sprintf("%.0f", numVal(p, "position")),
			updated,
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runWikiGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pageID := args[0]
	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}

	path := "/api/wiki-pages/" + url.PathEscape(pageID)
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var page map[string]any
	if err := client.GetJSON(ctx, path, &page); err != nil {
		return fmt.Errorf("get wiki page: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "SLUG", "CONTENT"}
		content := strVal(page, "content")
		if len(content) > 120 {
			content = content[:120] + "..."
		}
		rows := [][]string{{
			strVal(page, "id"),
			strVal(page, "title"),
			strVal(page, "slug"),
			content,
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, page)
}

func runWikiCreate(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}

	content, _, err := resolveTextFlag(cmd, "content")
	if err != nil {
		return err
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"title": title}
	if content != "" {
		body["content"] = content
	}
	if v, _ := cmd.Flags().GetString("parent"); v != "" {
		body["parent_id"] = v
	}
	if cmd.Flags().Changed("position") {
		v, _ := cmd.Flags().GetFloat64("position")
		body["position"] = v
	}

	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	path := "/api/wiki-pages"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.PostJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("create wiki page: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "SLUG"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "title"),
			strVal(result, "slug"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runWikiUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pageID := args[0]
	body := map[string]any{}

	if cmd.Flags().Changed("title") {
		v, _ := cmd.Flags().GetString("title")
		body["title"] = v
	}

	content, hasContent, err := resolveTextFlag(cmd, "content")
	if err != nil {
		return err
	}
	if hasContent {
		body["content"] = content
	}

	if cmd.Flags().Changed("position") {
		v, _ := cmd.Flags().GetFloat64("position")
		body["position"] = v
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use flags like --title, --content, --content-stdin, --content-file, --position")
	}

	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	path := "/api/wiki-pages/" + url.PathEscape(pageID)
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	var result map[string]any
	if err := client.PatchJSON(ctx, path, body, &result); err != nil {
		return fmt.Errorf("update wiki page: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "TITLE", "SLUG"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "title"),
			strVal(result, "slug"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runWikiDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pageID := args[0]
	params := url.Values{}
	if client.WorkspaceID != "" {
		params.Set("workspace_id", client.WorkspaceID)
	}
	path := "/api/wiki-pages/" + url.PathEscape(pageID)
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	if err := client.DeleteJSON(ctx, path); err != nil {
		return fmt.Errorf("delete wiki page: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, map[string]any{"deleted": true, "id": pageID})
	}

	fmt.Fprintf(os.Stdout, "Wiki page %s deleted.\n", pageID)
	return nil
}

// numVal extracts a float64 from a map; returns 0 if missing or wrong type.
func numVal(m map[string]any, key string) float64 {
	v, _ := m[key].(float64)
	return v
}
