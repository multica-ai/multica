package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dwickyfp/wallts/server/internal/cli"
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
	Short: "Get skill details (includes files)",
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
	Short: "Import a skill from a URL or local filesystem",
	RunE:  runSkillImport,
}

var skillUpdateFromSourceCmd = &cobra.Command{
	Use:   "update-source <id>",
	Short: "Update a skill by re-importing from its stored origin source",
	Args:  exactArgs(1),
	RunE:  runSkillUpdateFromSource,
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
	skillCmd.AddCommand(skillSearchCmd)
	skillCmd.AddCommand(skillUpdateFromSourceCmd)
	skillCmd.AddCommand(skillFilesCmd)

	skillFilesCmd.AddCommand(skillFilesListCmd)
	skillFilesCmd.AddCommand(skillFilesUpsertCmd)
	skillFilesCmd.AddCommand(skillFilesDeleteCmd)

	// skill list
	skillListCmd.Flags().String("output", "table", "Output format: table or json")

	// skill get
	skillGetCmd.Flags().String("output", "json", "Output format: table or json")

	// skill create
	skillCreateCmd.Flags().String("name", "", "Skill name (required)")
	skillCreateCmd.Flags().String("description", "", "Skill description")
	skillCreateCmd.Flags().String("content", "", "Skill content (SKILL.md body)")
	skillCreateCmd.Flags().String("config", "", "Skill config as JSON string")
	skillCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// skill update
	skillUpdateCmd.Flags().String("name", "", "New name")
	skillUpdateCmd.Flags().String("description", "", "New description")
	skillUpdateCmd.Flags().String("content", "", "New content")
	skillUpdateCmd.Flags().String("config", "", "New config as JSON string")
	skillUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	// skill delete
	skillDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")

	// skill import
	skillImportCmd.Flags().String("url", "", "URL to import from")
	skillImportCmd.Flags().String("path", "", "Local directory to import from")
	skillImportCmd.Flags().String("output", "json", "Output format: table or json")

	// skill update --from-source
	skillUpdateCmd.Flags().Bool("from-source", false, "Re-import from the stored origin source")

	// skill search
	skillSearchCmd.Flags().String("output", "json", "Output format: table or json")

	// skill files list
	skillFilesListCmd.Flags().String("output", "table", "Output format: table or json")

	// skill files upsert
	skillFilesUpsertCmd.Flags().String("path", "", "File path within the skill (required)")
	skillFilesUpsertCmd.Flags().String("content", "", "File content (required)")
	skillFilesUpsertCmd.Flags().String("output", "json", "Output format: table or json")
}

// ---------------------------------------------------------------------------
// Skill commands
// ---------------------------------------------------------------------------

func runSkillList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var skills []map[string]any
	if err := client.GetJSON(ctx, "/api/skills", &skills); err != nil {
		return fmt.Errorf("list skills: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, skills)
	}

	headers := []string{"ID", "NAME", "DESCRIPTION", "SOURCE", "CREATED_AT"}
	rows := make([][]string, 0, len(skills))
	for _, s := range skills {
		source := ""
		if config, ok := s["config"].(map[string]any); ok {
			if origin, ok := config["origin"].(map[string]any); ok {
				if t, ok := origin["type"].(string); ok {
					source = t
					if src, ok := origin["source_url"].(string); ok && src != "" {
						source = src
					}
				}
			}
		}
		rows = append(rows, []string{
			strVal(s, "id"),
			strVal(s, "name"),
			strVal(s, "description"),
			source,
			strVal(s, "created_at"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runSkillGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var skill map[string]any
	if err := client.GetJSON(ctx, "/api/skills/"+args[0], &skill); err != nil {
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
	if v, _ := cmd.Flags().GetString("content"); v != "" {
		body["content"] = v
	}
	if cmd.Flags().Changed("config") {
		v, _ := cmd.Flags().GetString("config")
		var config any
		if err := json.Unmarshal([]byte(v), &config); err != nil {
			return fmt.Errorf("--config must be valid JSON: %w", err)
		}
		body["config"] = config
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	if cmd.Flags().Changed("content") {
		v, _ := cmd.Flags().GetString("content")
		body["content"] = v
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/skills/"+args[0]); err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}

	fmt.Printf("Skill deleted: %s\n", args[0])
	return nil
}

func runSkillImport(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	importURL, _ := cmd.Flags().GetString("url")
	importPath, _ := cmd.Flags().GetString("path")

	if importURL == "" && importPath == "" {
		return fmt.Errorf("one of --url or --path is required")
	}
	if importURL != "" && importPath != "" {
		return fmt.Errorf("--url and --path are mutually exclusive")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if importPath != "" {
		return runSkillImportFromPath(cmd, client, ctx, importPath)
	}

	body := map[string]any{
		"url": importURL,
	}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/skills/import", body, &result); err != nil {
		if handleSkillImportConflict(cmd, err) {
			return nil
		}
		return fmt.Errorf("import skill: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Skill imported: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

// runSkillImportFromPath reads a local skill directory and imports it to the workspace.
func runSkillImportFromPath(cmd *cobra.Command, client *cli.APIClient, ctx context.Context, dirPath string) error {
	info, err := os.Stat(dirPath)
	if err != nil {
		return fmt.Errorf("cannot access path %s: %w", dirPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %s is not a directory", dirPath)
	}

	skillMdPath := filepath.Join(dirPath, "SKILL.md")
	skillMdBytes, err := os.ReadFile(skillMdPath)
	if err != nil {
		return fmt.Errorf("SKILL.md not found in %s: %w", dirPath, err)
	}
	skillMdContent := string(skillMdBytes)

	// Validate SKILL.md has frontmatter with at least a name.
	firstLine := strings.TrimSpace(strings.SplitN(skillMdContent, "\n", 2)[0])
	if firstLine != "---" {
		return fmt.Errorf("SKILL.md must start with YAML frontmatter (---); found: %q", firstLine)
	}

	// Collect supporting files (everything except SKILL.md).
	var files []map[string]string
	err = filepath.Walk(dirPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dirPath, path)
		if rel == "SKILL.md" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", rel, readErr)
		}
		files = append(files, map[string]string{
			"path":    filepath.ToSlash(rel),
			"content": string(data),
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking directory: %w", err)
	}

	// Build origin metadata.
	absPath, _ := filepath.Abs(dirPath)
	origin := map[string]any{
		"type": "local",
		"path": absPath,
	}

	body := map[string]any{
		"name":    filepath.Base(dirPath),
		"content": skillMdContent,
		"files":   files,
		"config":  map[string]any{"origin": origin},
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/skills", body, &result); err != nil {
		return fmt.Errorf("import from path: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Skill imported: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func handleSkillImportConflict(cmd *cobra.Command, err error) bool {
	var httpErr *cli.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusConflict || strings.TrimSpace(httpErr.Body) == "" {
		return false
	}

	var body map[string]any
	if json.Unmarshal([]byte(httpErr.Body), &body) != nil {
		return false
	}
	if _, ok := body["existing_skill"]; !ok {
		return false
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		_ = cli.PrintJSON(os.Stdout, body)
		return true
	}

	existing, _ := body["existing_skill"].(map[string]any)
	fmt.Printf("Skill already exists: %s (%s)\n", strVal(existing, "name"), strVal(existing, "id"))
	return true
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
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
// Update from source
// ---------------------------------------------------------------------------

func runSkillUpdateFromSource(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Fetch current skill to get origin metadata.
	var skill map[string]any
	if err := client.GetJSON(ctx, "/api/skills/"+args[0], &skill); err != nil {
		return fmt.Errorf("get skill: %w", err)
	}

	origin, _ := skill["config"].(map[string]any)
	if origin == nil {
		return fmt.Errorf("skill has no config; cannot update from source")
	}
	originData, _ := origin["origin"].(map[string]any)
	if originData == nil {
		return fmt.Errorf("skill has no origin metadata; cannot update from source")
	}

	originType, _ := originData["type"].(string)
	switch originType {
	case "local":
		localPath, _ := originData["path"].(string)
		if localPath == "" {
			return fmt.Errorf("local origin has no path")
		}
		return updateSkillFromLocalPath(cmd, client, ctx, args[0], skill, localPath)
	case "github", "clawhub", "skills.sh":
		sourceURL, _ := originData["source_url"].(string)
		if sourceURL == "" {
			return fmt.Errorf("origin has no source_url")
		}
		return updateSkillFromURL(cmd, client, ctx, args[0], skill, sourceURL)
	default:
		return fmt.Errorf("unknown origin type %q; cannot update from source", originType)
	}
}

func updateSkillFromLocalPath(cmd *cobra.Command, client *cli.APIClient, ctx context.Context, skillID string, existing map[string]any, localPath string) error {
	info, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("cannot access path %s: %w", localPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %s is not a directory", localPath)
	}

	skillMdBytes, err := os.ReadFile(filepath.Join(localPath, "SKILL.md"))
	if err != nil {
		return fmt.Errorf("SKILL.md not found in %s: %w", localPath, err)
	}

	var files []map[string]string
	err = filepath.Walk(localPath, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fi.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(localPath, path)
		if rel == "SKILL.md" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		files = append(files, map[string]string{
			"path":    filepath.ToSlash(rel),
			"content": string(data),
		})
		return nil
	})
	if err != nil {
		return fmt.Errorf("walking directory: %w", err)
	}

	body := map[string]any{
		"content": string(skillMdBytes),
		"files":   files,
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/skills/"+skillID, body, &result); err != nil {
		return fmt.Errorf("update skill from local path: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Skill updated from source: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

func updateSkillFromURL(cmd *cobra.Command, client *cli.APIClient, ctx context.Context, skillID string, existing map[string]any, sourceURL string) error {
	// Re-import from the URL source, then update the existing skill.
	body := map[string]any{
		"url": sourceURL,
	}

	// Use the import endpoint to fetch fresh content.
	var imported map[string]any
	if err := client.PostJSON(ctx, "/api/skills/import", body, &imported); err != nil {
		return fmt.Errorf("re-import from source URL: %w", err)
	}

	// Now update the existing skill with the fresh content.
	updateBody := map[string]any{
		"content": strVal(imported, "content"),
		"config":  imported["config"],
	}
	if desc := strVal(imported, "description"); desc != "" {
		updateBody["description"] = desc
	}
	// Include files from the imported skill.
	if files, ok := imported["files"].([]any); ok && len(files) > 0 {
		updateBody["files"] = files
	}

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/skills/"+skillID, updateBody, &result); err != nil {
		return fmt.Errorf("update skill from source: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}
	fmt.Printf("Skill updated from source: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
	return nil
}

// ---------------------------------------------------------------------------
// Skill file subcommands
// ---------------------------------------------------------------------------

func runSkillFilesList(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var files []map[string]any
	if err := client.GetJSON(ctx, "/api/skills/"+args[0]+"/files", &files); err != nil {
		return fmt.Errorf("list skill files: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, files)
	}

	headers := []string{"ID", "PATH", "CREATED_AT", "UPDATED_AT"}
	rows := make([][]string, 0, len(files))
	for _, f := range files {
		rows = append(rows, []string{
			strVal(f, "id"),
			strVal(f, "path"),
			strVal(f, "created_at"),
			strVal(f, "updated_at"),
		})
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
	content, _ := cmd.Flags().GetString("content")
	if content == "" {
		return fmt.Errorf("--content is required")
	}

	body := map[string]any{
		"path":    filePath,
		"content": content,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/skills/"+args[0]+"/files/"+args[1]); err != nil {
		return fmt.Errorf("delete skill file: %w", err)
	}

	fmt.Printf("Skill file deleted: %s\n", args[1])
	return nil
}
