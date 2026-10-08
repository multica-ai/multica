package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Work with skills",
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List skills in the workspace",
	RunE:  runSkillList,
}

var skillGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get skill details and its file list (use --with-content for the bodies)",
	Args:  exactArgs(1),
	RunE:  runSkillGet,
}

var skillCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new skill",
	RunE:  runSkillCreate,
}

var skillUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a skill",
	Args:  exactArgs(1),
	RunE:  runSkillUpdate,
}

var skillDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a skill",
	Args:  exactArgs(1),
	RunE:  runSkillDelete,
}

var skillImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import a skill from a URL (clawhub.ai, skills.sh, github.com) or a local .skill/.zip archive",
	RunE:  runSkillImport,
}

var skillRefreshCmd = &cobra.Command{
	Use:   "refresh <id>",
	Short: "Re-download a skill from its imported source, preserving its id and agent assignments",
	Args:  exactArgs(1),
	RunE:  runSkillRefresh,
}

var skillSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search for installable skills",
	Args:  exactArgs(1),
	RunE:  runSkillSearch,
}

// Skill file subcommands.

var skillFilesCmd = &cobra.Command{
	Use:   "files",
	Short: "Work with skill files",
}

var skillFilesListCmd = &cobra.Command{
	Use:   "list <skill-id>",
	Short: "List files for a skill",
	Args:  exactArgs(1),
	RunE:  runSkillFilesList,
}

var skillFilesUpsertCmd = &cobra.Command{
	Use:   "upsert <skill-id>",
	Short: "Create or update a skill file",
	Args:  exactArgs(1),
	RunE:  runSkillFilesUpsert,
}

var skillFilesDeleteCmd = &cobra.Command{
	Use:   "delete <skill-id> <file-id>",
	Short: "Delete a skill file",
	Args:  exactArgs(2),
	RunE:  runSkillFilesDelete,
}

func init() {
	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillGetCmd)
	skillCmd.AddCommand(skillCreateCmd)
	skillCmd.AddCommand(skillUpdateCmd)
	skillCmd.AddCommand(skillDeleteCmd)
	skillCmd.AddCommand(skillImportCmd)
	skillCmd.AddCommand(skillRefreshCmd)
	skillCmd.AddCommand(skillSearchCmd)
	skillCmd.AddCommand(skillFilesCmd)

	skillFilesCmd.AddCommand(skillFilesListCmd)
	skillFilesCmd.AddCommand(skillFilesUpsertCmd)
	skillFilesCmd.AddCommand(skillFilesDeleteCmd)

	// skill list
	skillListCmd.Flags().String("output", "table", "Output format: table or json")

	// skill get
	skillGetCmd.Flags().String("output", "json", "Output format: table or json")
	skillGetCmd.Flags().Bool("with-content", false, "Include the SKILL.md body and every file body. Off by default: the response grows with the skill and large skills cannot be fetched this way over slow links.")

	// skill create
	skillCreateCmd.Flags().String("name", "", "Skill name (required)")
	skillCreateCmd.Flags().String("description", "", "Skill description")
	skillCreateCmd.Flags().String("content", "", "Skill content (SKILL.md body)")
	skillCreateCmd.Flags().Bool("content-stdin", false, "Read skill content from stdin. Mutually exclusive with --content and --content-file.")
	skillCreateCmd.Flags().String("content-file", "", "Read skill content from a UTF-8 file. Mutually exclusive with --content and --content-stdin.")
	skillCreateCmd.Flags().String("config", "", "Skill config as JSON string")
	skillCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// skill update
	skillUpdateCmd.Flags().String("name", "", "New name")
	skillUpdateCmd.Flags().String("description", "", "New description")
	skillUpdateCmd.Flags().String("content", "", "New content")
	skillUpdateCmd.Flags().Bool("content-stdin", false, "Read new content from stdin. Mutually exclusive with --content and --content-file.")
	skillUpdateCmd.Flags().String("content-file", "", "Read new content from a UTF-8 file. Mutually exclusive with --content and --content-stdin.")
	skillUpdateCmd.Flags().String("config", "", "New config as JSON string")
	skillUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// skill delete
	skillDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")

	// skill import
	skillImportCmd.Flags().String("url", "", "URL to import from (clawhub.ai, skills.sh, or github.com). Mutually exclusive with --file.")
	skillImportCmd.Flags().String("file", "", "Path to a local skill archive (.skill or .zip) to import. Mutually exclusive with --url.")
	skillImportCmd.Flags().String("on-conflict", "fail", "Conflict strategy when a skill with the same name exists: fail, overwrite, rename, or skip")
	skillImportCmd.Flags().String("output", "json", "Output format: table or json")

	// skill refresh
	skillRefreshCmd.Flags().String("output", "json", "Output format: table or json")

	// skill search
	skillSearchCmd.Flags().String("output", "json", "Output format: table or json")

	// skill files list
	skillFilesListCmd.Flags().String("output", "table", "Output format: table or json")
	skillFilesListCmd.Flags().Bool("with-content", false, "Include each file's body. Off by default: use it to read a file, not to list them.")

	// skill files upsert
	skillFilesUpsertCmd.Flags().String("path", "", "File path within the skill (required)")
	skillFilesUpsertCmd.Flags().String("content", "", "File content (required)")
	skillFilesUpsertCmd.Flags().Bool("content-stdin", false, "Read file content from stdin. Mutually exclusive with --content and --content-file.")
	skillFilesUpsertCmd.Flags().String("content-file", "", "Read file content from a UTF-8 file. Mutually exclusive with --content and --content-stdin.")
	skillFilesUpsertCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Skill commands
// ---------------------------------------------------------------------------

// newSkillAPIClient is newAPIClient with the transport swapped for the
// stall-aware one (see internal/cli/stall.go). Skill payloads are the largest
// responses the CLI reads, so they are where a total-elapsed deadline breaks
// first — a 599KB skill could not be fetched at all over a link that was
// delivering it perfectly well, just not within 30s (GH #7498).
//
// It reuses newAPIClient rather than duplicating credential and header
// resolution: this is a pilot of which commands adopt the mechanism, not a
// second CLI client. Graduating it means moving the transport into
// cli.NewAPIClient and deleting this function.
func newSkillAPIClient(cmd *cobra.Command) (*cli.APIClient, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, err
	}
	client.HTTPClient = cli.NewStallAwareHTTPClient()
	return client, nil
}

// skillFileSizeCell renders the `size` field of a file-metadata response.
//
// It cannot go through strVal: JSON numbers decode as float64, and strVal's
// %v prints a 1.2MB file as "1.234567e+06" — unreadable exactly when the file
// is big enough to be the one you are looking for. formatBytes is the same
// renderer the daemon table uses.
func skillFileSizeCell(m map[string]any) string {
	size, ok := m["size"].(float64)
	if !ok {
		return strVal(m, "size")
	}
	return formatBytes(int64(size))
}

// skillIncludeQuery renders the ?include= parameter the skill endpoints take.
// The CLI asks for metadata by default: `skill get`'s table view prints four
// columns, and `skill files list` prints none of the file bodies, so pulling
// every byte of content to render them was pure cost — and, past a few hundred
// KB, the reason neither command completed.
func skillIncludeQuery(cmd *cobra.Command) string {
	if withContent, _ := cmd.Flags().GetBool("with-content"); withContent {
		return "?include=content"
	}
	return "?include=metadata"
}

// resolveSkillContentFlag intentionally stays separate from resolveTextFlag.
// Skill bodies are Markdown documents where byte-level preservation matters:
// inline --content is not backslash-unescaped, and stdin/file input is not
// trimmed, so agents can round-trip generated SKILL.md content exactly.
func resolveSkillContentFlag(cmd *cobra.Command) (string, bool, error) {
	useStdin, _ := cmd.Flags().GetBool("content-stdin")
	inline, _ := cmd.Flags().GetString("content")
	filePath, _ := cmd.Flags().GetString("content-file")
	inlineSet := cmd.Flags().Changed("content")

	sources := 0
	if inlineSet {
		sources++
	}
	if useStdin {
		sources++
	}
	if filePath != "" {
		sources++
	}
	if sources > 1 {
		return "", false, fmt.Errorf("--content, --content-stdin, and --content-file are mutually exclusive")
	}

	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", false, fmt.Errorf("read stdin for --content-stdin: %w", err)
		}
		return skillContentBytesToString(data, "stdin content for --content-stdin")
	}
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", false, fmt.Errorf("read file for --content-file: %w", err)
		}
		return skillContentBytesToString(data, "file content for --content-file")
	}
	if inlineSet {
		return inline, true, nil
	}
	return "", false, nil
}

func skillContentBytesToString(data []byte, label string) (string, bool, error) {
	if len(data) == 0 {
		return "", false, fmt.Errorf("%s is empty", label)
	}
	if !utf8.Valid(data) {
		return "", false, fmt.Errorf("%s must be valid UTF-8", label)
	}
	return string(data), true, nil
}

func runSkillList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var skills []map[string]any
	if err := client.GetJSON(ctx, "/api/skills", &skills); err != nil {
		return fmt.Errorf("list skills: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, skills)
	}

	headers := []string{"ID", "NAME", "DESCRIPTION", "CREATED_AT"}
	rows := make([][]string, 0, len(skills))
	for _, s := range skills {
		rows = append(rows, []string{
			strVal(s, "id"),
			strVal(s, "name"),
			strVal(s, "description"),
			strVal(s, "created_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runSkillGet(cmd *cobra.Command, args []string) error {
	client, err := newSkillAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.StallAwareContext(context.Background())
	defer cancel()

	var skill map[string]any
	if err := client.GetJSON(ctx, "/api/skills/"+args[0]+skillIncludeQuery(cmd), &skill); err != nil {
		return fmt.Errorf("get skill: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, skill)
	}

	headers := []string{"ID", "NAME", "DESCRIPTION", "CREATED_AT"}
	rows := [][]string{{
		strVal(skill, "id"),
		strVal(skill, "name"),
		strVal(skill, "description"),
		strVal(skill, "created_at"),
	}}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runSkillCreate(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}

	body := map[string]any{
		"name": name,
	}
	if v, _ := cmd.Flags().GetString("description"); v != "" {
		body["description"] = v
	}
	content, hasContent, err := resolveSkillContentFlag(cmd)
	if err != nil {
		return err
	}
	if hasContent && content != "" {
		body["content"] = content
	}
	if cmd.Flags().Changed("config") {
		v, _ := cmd.Flags().GetString("config")
		var config any
		if err := json.Unmarshal([]byte(v), &config); err != nil {
			return fmt.Errorf("--config must be valid JSON: %w", err)
		}
		body["config"] = config
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/skills", body, &result); err != nil {
		return fmt.Errorf("create skill: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Skill created: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runSkillUpdate(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	body := map[string]any{}
	if cmd.Flags().Changed("name") {
		v, _ := cmd.Flags().GetString("name")
		body["name"] = v
	}
	if cmd.Flags().Changed("description") {
		v, _ := cmd.Flags().GetString("description")
		body["description"] = v
	}
	content, hasContent, err := resolveSkillContentFlag(cmd)
	if err != nil {
		return err
	}
	if hasContent {
		body["content"] = content
	}
	if cmd.Flags().Changed("config") {
		v, _ := cmd.Flags().GetString("config")
		var config any
		if err := json.Unmarshal([]byte(v), &config); err != nil {
			return fmt.Errorf("--config must be valid JSON: %w", err)
		}
		body["config"] = config
	}

	if len(body) == 0 {
		return fmt.Errorf("no fields to update; use --name, --description, --content, or --config")
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/skills/"+args[0], body, &result); err != nil {
		return fmt.Errorf("update skill: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Skill updated: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runSkillDelete(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		fmt.Printf("Are you sure you want to delete skill %s? This cannot be undone. [y/N] ", args[0])
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/skills/"+args[0]); err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}

	fmt.Printf("Skill deleted: %s\n", args[0])
	return nil
}

func runSkillRefresh(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	// The server re-fetches the bundle from the upstream source before
	// answering; give it the same budget as an import (server-side cap: 45s).
	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/skills/"+args[0]+"/refresh", map[string]any{}, &result); err != nil {
		return fmt.Errorf("refresh skill: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Skill updated from source: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func runSkillImport(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	importURL, _ := cmd.Flags().GetString("url")
	importFile, _ := cmd.Flags().GetString("file")
	switch {
	case importURL == "" && importFile == "":
		return fmt.Errorf("either --url or --file is required")
	case importURL != "" && importFile != "":
		return fmt.Errorf("--url and --file are mutually exclusive")
	}
	onConflict, _ := cmd.Flags().GetString("on-conflict")
	if !validSkillImportConflictStrategy(onConflict) {
		return fmt.Errorf("--on-conflict must be one of: fail, overwrite, rename, skip")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	var result map[string]any
	if importFile != "" {
		fileData, readErr := os.ReadFile(importFile)
		if readErr != nil {
			return fmt.Errorf("read skill archive: %w", readErr)
		}
		if err := client.ImportSkillFile(ctx, fileData, filepath.Base(importFile), onConflict, &result); err != nil {
			if handledErr := handleSkillImportError(cmd, err); handledErr != nil {
				return handledErr
			}
			return fmt.Errorf("import skill: %w", err)
		}
		return printSkillImportResult(cmd, result)
	}

	body := map[string]any{
		"url":         importURL,
		"on_conflict": onConflict,
	}
	if err := client.PostJSON(ctx, "/api/skills/import", body, &result); err != nil {
		if handledErr := handleSkillImportError(cmd, err); handledErr != nil {
			return handledErr
		}
		return fmt.Errorf("import skill: %w", err)
	}

	return printSkillImportResult(cmd, result)
}

func validSkillImportConflictStrategy(strategy string) bool {
	switch strategy {
	case "fail", "overwrite", "rename", "skip":
		return true
	}
	return false
}

func handleSkillImportError(cmd *cobra.Command, err error) error {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || strings.TrimSpace(httpErr.Body) == "" {
		return nil
	}

	var body map[string]any
	if json.Unmarshal([]byte(httpErr.Body), &body) != nil {
		return nil
	}
	if _, ok := body["status"]; !ok {
		if _, hasExisting := body["existing_skill"]; !hasExisting {
			return nil
		}
		body = normalizeLegacySkillImportConflict(body)
	}

	if err := printSkillImportResult(cmd, body); err != nil {
		return err
	}
	reason := strVal(body, "reason")
	if reason == "" {
		reason = strVal(body, "error")
	}
	if reason == "" {
		reason = "skill import conflict"
	}
	return errors.New(reason)
}

func normalizeLegacySkillImportConflict(body map[string]any) map[string]any {
	reason := strVal(body, "error")
	if reason == "" {
		reason = "a skill with this name already exists"
	}
	reason += "; use --on-conflict overwrite to replace it or --on-conflict rename to import a copy"
	return map[string]any{
		"status":         "conflict",
		"reason":         reason,
		"existing_skill": body["existing_skill"],
	}
}

func printSkillImportResult(cmd *cobra.Command, result map[string]any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	status := strVal(result, "status")
	if status == "" {
		fmt.Printf("Skill imported: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
		return nil
	}

	skill := nestedMap(result, "skill")
	existing := nestedMap(result, "existing_skill")
	reason := strVal(result, "reason")
	switch status {
	case "created":
		fmt.Printf("Skill imported: %s (%s)\n", strVal(skill, "name"), strVal(skill, "id"))
	case "updated":
		fmt.Printf("Skill updated: %s (%s)\n", strVal(skill, "name"), strVal(skill, "id"))
	case "skipped":
		fmt.Printf("Skill skipped: %s (%s)\n", strVal(existing, "name"), strVal(existing, "id"))
	case "conflict":
		fmt.Printf("Skill import conflict: %s (%s)\n", strVal(existing, "name"), strVal(existing, "id"))
	case "failed":
		fmt.Printf("Skill import failed: %s\n", reason)
	default:
		fmt.Printf("Skill import %s\n", status)
	}
	if reason != "" && status != "failed" {
		fmt.Printf("Reason: %s\n", reason)
	}
	return nil
}

func nestedMap(m map[string]any, key string) map[string]any {
	nested, _ := m[key].(map[string]any)
	if nested == nil {
		return map[string]any{}
	}
	return nested
}

func runSkillSearch(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	query := strings.TrimSpace(args[0])
	if query == "" {
		return fmt.Errorf("query is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), cli.AtLeastAPITimeout(60*time.Second))
	defer cancel()

	var results []map[string]any
	path := "/api/skills/search?q=" + url.QueryEscape(query)
	if err := client.GetJSON(ctx, path, &results); err != nil {
		return fmt.Errorf("search skills: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, results)
	}

	headers := []string{"NAME", "URL", "SOURCE", "INSTALLS", "DESCRIPTION"}
	rows := make([][]string, 0, len(results))
	for _, result := range results {
		rows = append(rows, []string{
			strVal(result, "name"),
			strVal(result, "url"),
			strVal(result, "source"),
			strVal(result, "install_count"),
			strVal(result, "description"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

// ---------------------------------------------------------------------------
// Skill file subcommands
// ---------------------------------------------------------------------------

func runSkillFilesList(cmd *cobra.Command, args []string) error {
	client, err := newSkillAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.StallAwareContext(context.Background())
	defer cancel()

	var files []map[string]any
	if err := client.GetJSON(ctx, "/api/skills/"+args[0]+"/files"+skillIncludeQuery(cmd), &files); err != nil {
		return fmt.Errorf("list skill files: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, files)
	}

	// SIZE is what makes an oversized skill diagnosable: it names the file to
	// look at without downloading any of them. The content shape carries no
	// size field, so the column is dropped under --with-content rather than
	// rendered empty.
	withContent, _ := cmd.Flags().GetBool("with-content")
	headers := []string{"ID", "PATH", "SIZE", "CREATED_AT", "UPDATED_AT"}
	if withContent {
		headers = []string{"ID", "PATH", "CREATED_AT", "UPDATED_AT"}
	}
	rows := make([][]string, 0, len(files))
	for _, f := range files {
		row := []string{strVal(f, "id"), strVal(f, "path")}
		if !withContent {
			row = append(row, skillFileSizeCell(f))
		}
		row = append(row, strVal(f, "created_at"), strVal(f, "updated_at"))
		rows = append(rows, row)
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runSkillFilesUpsert(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	filePath, _ := cmd.Flags().GetString("path")
	if filePath == "" {
		return fmt.Errorf("--path is required")
	}
	content, hasContent, err := resolveSkillContentFlag(cmd)
	if err != nil {
		return err
	}
	if !hasContent || content == "" {
		return fmt.Errorf("--content is required")
	}

	body := map[string]any{
		"path":    filePath,
		"content": content,
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/skills/"+args[0]+"/files", body, &result); err != nil {
		return fmt.Errorf("upsert skill file: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Skill file upserted: %s (%s)\n", strVal(result, "path"), strVal(result, "id"))
	return nil
}

func runSkillFilesDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/skills/"+args[0]+"/files/"+args[1]); err != nil {
		return fmt.Errorf("delete skill file: %w", err)
	}

	fmt.Printf("Skill file deleted: %s\n", args[1])
	return nil
}
