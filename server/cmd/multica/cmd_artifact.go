package main

// CEREBRO-PATCH(artifact-cli-cmd-artifact): cerebro modification of upstream file

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var artifactCmd = &cobra.Command{
	Use:   "artifact",
	Short: "Work with artifacts (documents, reports, plans, decisions)",
	Long: `Artifacts are durable documents with a title and body — distinct from comments and file attachments.
Use these commands to create, read, update, list, or delete artifacts produced by you or other agents.`,
}

var artifactCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new artifact (document)",
	Long:  "Create a new artifact. Body is required for md/html format and can be passed via --body or --body-stdin.",
	Example: `  # Create a workspace-level note from stdin
  $ cat report.md | multica artifact create --kind report --title "Q2 review" --body-stdin

  # Create a plan scoped to an issue
  $ multica artifact create --kind plan --title "Migration plan" --body "..." --issue abc123

  # Create a project-scoped decision record
  $ multica artifact create --kind decision --title "ADR 7: auth" --body-stdin --project def456 < adr.md`,
	RunE: runArtifactCreate,
}

var artifactGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Fetch a single artifact by ID",
	Args:  exactArgs(1),
	RunE:  runArtifactGet,
}

var artifactUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an artifact's title or body",
	Long:  "Update title and/or body of an existing artifact. At least one of --title, --body, --body-stdin must be set.",
	Example: `  # Replace body from stdin
  $ cat new-content.md | multica artifact update <id> --body-stdin

  # Change title only
  $ multica artifact update <id> --title "New title"`,
	Args: exactArgs(1),
	RunE: runArtifactUpdate,
}

var artifactListCmd = &cobra.Command{
	Use:   "list",
	Short: "List artifacts on an issue or project",
	Long:  "List artifacts scoped to an issue or a project. Provide either --issue or --project.",
	RunE:  runArtifactList,
}

var artifactDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete an artifact",
	Args:  exactArgs(1),
	RunE:  runArtifactDelete,
}

var artifactSetFolderCmd = &cobra.Command{
	Use:   "set-folder <id>",
	Short: "Move an artifact into a folder (or to root)",
	Long:  "Place an artifact in a folder, or move it to root by passing --folder \"\" (or omitting --folder).",
	Example: `  # Move artifact into folder
  $ multica artifact set-folder abc123 --folder fld456

  # Move artifact back to root
  $ multica artifact set-folder abc123 --folder ""`,
	Args: exactArgs(1),
	RunE: runArtifactSetFolder,
}

var artifactFolderCmd = &cobra.Command{
	Use:   "folder",
	Short: "Manage artifact folders",
	Long:  "Folders group artifacts inside a workspace. They are workspace-wide and orthogonal to issue/project scope.",
}

var artifactFolderListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all artifact folders in the workspace",
	Long:  "List every folder in the workspace. Returns id, parent_id, name. Build the tree client-side via parent_id; null parent_id is a root folder.",
	RunE:  runArtifactFolderList,
}

var artifactFolderCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an artifact folder",
	Long:  "Create a folder for grouping artifacts. Folder names must be unique within their parent.",
	Example: `  # Create a root folder
  $ multica artifact folder create --name "Daily sales reports"

  # Create a nested folder
  $ multica artifact folder create --name "2026" --parent <parent-folder-id>`,
	RunE: runArtifactFolderCreate,
}

var artifactFolderUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Rename or reparent an artifact folder",
	Long:  "Update an artifact folder. At least one of --name or --parent must be set. Pass --parent \"\" to move to root.",
	Args:  exactArgs(1),
	RunE:  runArtifactFolderUpdate,
}

var artifactFolderDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete an artifact folder",
	Long:  "Delete a folder. Subfolders are deleted along with it. Artifacts inside fall back to the root.",
	Args:  exactArgs(1),
	RunE:  runArtifactFolderDelete,
}

func init() {
	artifactCmd.AddCommand(artifactCreateCmd)
	artifactCmd.AddCommand(artifactGetCmd)
	artifactCmd.AddCommand(artifactUpdateCmd)
	artifactCmd.AddCommand(artifactListCmd)
	artifactCmd.AddCommand(artifactDeleteCmd)
	artifactCmd.AddCommand(artifactSetFolderCmd)
	artifactCmd.AddCommand(artifactFolderCmd)

	artifactFolderCmd.AddCommand(artifactFolderListCmd)
	artifactFolderCmd.AddCommand(artifactFolderCreateCmd)
	artifactFolderCmd.AddCommand(artifactFolderUpdateCmd)
	artifactFolderCmd.AddCommand(artifactFolderDeleteCmd)

	artifactCreateCmd.Flags().String("kind", "note", "Artifact kind: report, plan, decision, diagram, note")
	artifactCreateCmd.Flags().String("title", "", "Artifact title (required)")
	artifactCreateCmd.Flags().String("body", "", "Artifact body (markdown or html). Use --body-stdin for multi-line / special chars.")
	artifactCreateCmd.Flags().Bool("body-stdin", false, "Read body from stdin")
	artifactCreateCmd.Flags().String("format", "md", "Body format: md or html")
	artifactCreateCmd.Flags().String("issue", "", "Scope to this issue ID (mutually exclusive with --project)")
	artifactCreateCmd.Flags().String("project", "", "Scope to this project ID (mutually exclusive with --issue)")
	artifactCreateCmd.Flags().String("folder", "", "Place the artifact in this folder ID")
	artifactCreateCmd.Flags().String("origin-issue", "", "Issue this artifact was produced for (preserves origin trail)")
	artifactCreateCmd.Flags().String("output", "json", "Output format: table or json")

	artifactGetCmd.Flags().String("output", "json", "Output format: table or json")

	artifactUpdateCmd.Flags().String("title", "", "New title")
	artifactUpdateCmd.Flags().String("body", "", "New body. Use --body-stdin for multi-line / special chars.")
	artifactUpdateCmd.Flags().Bool("body-stdin", false, "Read new body from stdin")
	artifactUpdateCmd.Flags().String("output", "json", "Output format: table or json")

	artifactListCmd.Flags().String("issue", "", "List artifacts scoped to this issue")
	artifactListCmd.Flags().String("project", "", "List artifacts scoped to this project")
	artifactListCmd.Flags().String("output", "json", "Output format: table or json")

	artifactSetFolderCmd.Flags().String("folder", "", "Target folder ID. Pass an empty string (or omit) to move to root.")
	artifactSetFolderCmd.Flags().String("output", "json", "Output format: table or json")

	artifactFolderListCmd.Flags().String("output", "json", "Output format: table or json")

	artifactFolderCreateCmd.Flags().String("name", "", "Folder name (required)")
	artifactFolderCreateCmd.Flags().String("parent", "", "Optional parent folder ID for nesting")
	artifactFolderCreateCmd.Flags().String("output", "json", "Output format: table or json")

	artifactFolderUpdateCmd.Flags().String("name", "", "New folder name")
	artifactFolderUpdateCmd.Flags().String("parent", "", "New parent folder ID. Pass an empty string to move to root.")
	artifactFolderUpdateCmd.Flags().String("output", "json", "Output format: table or json")
}

func runArtifactCreate(cmd *cobra.Command, _ []string) error {
	kind, _ := cmd.Flags().GetString("kind")
	title, _ := cmd.Flags().GetString("title")
	if title == "" {
		return fmt.Errorf("--title is required")
	}
	format, _ := cmd.Flags().GetString("format")
	if format == "" {
		format = "md"
	}

	body, _ := cmd.Flags().GetString("body")
	useStdin, _ := cmd.Flags().GetBool("body-stdin")
	if body != "" && useStdin {
		return fmt.Errorf("--body and --body-stdin are mutually exclusive")
	}
	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		body = string(data)
	}
	if body == "" {
		return fmt.Errorf("--body or --body-stdin is required")
	}

	issueID, _ := cmd.Flags().GetString("issue")
	projectID, _ := cmd.Flags().GetString("project")
	if issueID != "" && projectID != "" {
		return fmt.Errorf("provide at most one of --issue or --project")
	}

	req := map[string]any{
		"kind":   kind,
		"format": format,
		"title":  title,
		"body":   body,
	}
	if issueID != "" {
		req["issue_id"] = issueID
	}
	if projectID != "" {
		req["project_id"] = projectID
	}
	if v, _ := cmd.Flags().GetString("folder"); v != "" {
		req["folder_id"] = v
	}
	if v, _ := cmd.Flags().GetString("origin-issue"); v != "" {
		req["origin_issue_id"] = v
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/artifacts", req, &result); err != nil {
		return fmt.Errorf("create artifact: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runArtifactGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.GetJSON(ctx, "/api/artifacts/"+args[0], &result); err != nil {
		return fmt.Errorf("get artifact: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runArtifactUpdate(cmd *cobra.Command, args []string) error {
	title, _ := cmd.Flags().GetString("title")
	body, _ := cmd.Flags().GetString("body")
	useStdin, _ := cmd.Flags().GetBool("body-stdin")
	if body != "" && useStdin {
		return fmt.Errorf("--body and --body-stdin are mutually exclusive")
	}
	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		body = string(data)
	}

	req := map[string]any{}
	if title != "" {
		req["title"] = title
	}
	if body != "" || useStdin {
		req["body"] = body
	}
	if len(req) == 0 {
		return fmt.Errorf("nothing to update; provide --title, --body, or --body-stdin")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/artifacts/"+args[0], req, &result); err != nil {
		return fmt.Errorf("update artifact: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runArtifactList(cmd *cobra.Command, _ []string) error {
	issueID, _ := cmd.Flags().GetString("issue")
	projectID, _ := cmd.Flags().GetString("project")
	if issueID == "" && projectID == "" {
		return fmt.Errorf("provide either --issue or --project")
	}
	if issueID != "" && projectID != "" {
		return fmt.Errorf("provide either --issue or --project, not both")
	}

	var path string
	if issueID != "" {
		path = "/api/issues/" + issueID + "/artifacts"
	} else {
		path = "/api/projects/" + projectID + "/artifacts"
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result []map[string]any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return fmt.Errorf("list artifacts: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runArtifactDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/artifacts/"+args[0]); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Deleted artifact %s\n", args[0])
	return nil
}

func runArtifactSetFolder(cmd *cobra.Command, args []string) error {
	folderID, _ := cmd.Flags().GetString("folder")

	req := map[string]any{"folder_id": nil}
	if folderID != "" {
		req["folder_id"] = folderID
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/artifacts/"+args[0]+"/folder", req, &result); err != nil {
		return fmt.Errorf("move artifact: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runArtifactFolderList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result []map[string]any
	if err := client.GetJSON(ctx, "/api/artifact-folders", &result); err != nil {
		return fmt.Errorf("list artifact folders: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runArtifactFolderCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}

	req := map[string]any{"name": name}
	if parent, _ := cmd.Flags().GetString("parent"); parent != "" {
		req["parent_id"] = parent
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/artifact-folders", req, &result); err != nil {
		return fmt.Errorf("create artifact folder: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runArtifactFolderUpdate(cmd *cobra.Command, args []string) error {
	req := map[string]any{}
	if cmd.Flags().Changed("name") {
		name, _ := cmd.Flags().GetString("name")
		req["name"] = name
	}
	if cmd.Flags().Changed("parent") {
		parent, _ := cmd.Flags().GetString("parent")
		req["parent_id"] = parent
	}
	if len(req) == 0 {
		return fmt.Errorf("nothing to update; provide --name or --parent")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var result map[string]any
	if err := client.PutJSON(ctx, "/api/artifact-folders/"+args[0], req, &result); err != nil {
		return fmt.Errorf("update artifact folder: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func runArtifactFolderDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/artifact-folders/"+args[0]); err != nil {
		return fmt.Errorf("delete artifact folder: %w", err)
	}
	fmt.Fprintf(os.Stderr, "Deleted artifact folder %s\n", args[0])
	return nil
}
