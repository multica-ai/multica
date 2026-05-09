package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

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
	Short: "Import a skill from a URL (clawhub.ai or skills.sh)",
	RunE:  runSkillImport,
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
	skillImportCmd.Flags().String("url", "", "URL to import from (required)")
	skillImportCmd.Flags().String("output", "json", "Output format: table or json")

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
	if importURL == "" {
		return fmt.Errorf("--url is required")
	}

	body := map[string]any{
		"url": importURL,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/skills/import", body, &result); err != nil {
		return fmt.Errorf("import skill: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, result)
	}

	fmt.Printf("Skill imported: %s (%s)\n", strVal(result, "name"), strVal(result, "id"))
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
