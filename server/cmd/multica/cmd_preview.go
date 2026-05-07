package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var previewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Manage preview environments",
}

var previewCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a preview environment",
	Long: `Create a preview environment for the given app and issue.

The --image-tag flag specifies the tag of the container image to deploy.
Images are pulled from ghcr.io/g2crowd/ue-dev-arm64:<image_tag>.

Example:
  multica preview create --app ue --issue-id <id> --repo https://github.com/g2crowd/ue --image-tag abc1234`,
	RunE: runPreviewCreate,
}

var previewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List preview environments",
	RunE:  runPreviewList,
}

var previewGetCmd = &cobra.Command{
	Use:   "get <preview-id>",
	Short: "Get a preview environment",
	Args:  exactArgs(1),
	RunE:  runPreviewGet,
}

var previewDeleteCmd = &cobra.Command{
	Use:   "delete <preview-id>",
	Short: "Delete a preview environment",
	Args:  exactArgs(1),
	RunE:  runPreviewDelete,
}

func init() {
	previewCmd.AddCommand(previewCreateCmd)
	previewCmd.AddCommand(previewListCmd)
	previewCmd.AddCommand(previewGetCmd)
	previewCmd.AddCommand(previewDeleteCmd)

	// preview create
	previewCreateCmd.Flags().String("app", "", "Application name (e.g. ue) (required)")
	previewCreateCmd.Flags().String("issue-id", "", "Issue ID to associate with the preview (required)")
	previewCreateCmd.Flags().String("repo", "", "Repository URL (required)")
	previewCreateCmd.Flags().String("image-tag", "", "Image tag to deploy (required); image: ghcr.io/g2crowd/ue-dev-arm64:<image_tag>")
	previewCreateCmd.Flags().String("output", "json", "Output format: table or json")

	// preview list
	previewListCmd.Flags().String("output", "table", "Output format: table or json")

	// preview get
	previewGetCmd.Flags().String("output", "json", "Output format: table or json")

	// preview delete — no extra flags needed
}

// ---------------------------------------------------------------------------
// Preview commands
// ---------------------------------------------------------------------------

func runPreviewCreate(cmd *cobra.Command, _ []string) error {
	app, _ := cmd.Flags().GetString("app")
	if app == "" {
		return fmt.Errorf("--app is required")
	}
	issueID, _ := cmd.Flags().GetString("issue-id")
	if issueID == "" {
		return fmt.Errorf("--issue-id is required")
	}
	repo, _ := cmd.Flags().GetString("repo")
	if repo == "" {
		return fmt.Errorf("--repo is required")
	}
	imageTag, _ := cmd.Flags().GetString("image-tag")
	if imageTag == "" {
		return fmt.Errorf("--image-tag is required")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body := map[string]any{
		"app":       app,
		"issue_id":  issueID,
		"repo":      repo,
		"image_tag": imageTag,
	}

	var result map[string]any
	if err := client.PostJSON(ctx, "/api/previews", body, &result); err != nil {
		return fmt.Errorf("create preview: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "APP", "NAMESPACE", "HOST", "STATUS"}
		rows := [][]string{{
			strVal(result, "id"),
			strVal(result, "app"),
			strVal(result, "namespace_name"),
			strVal(result, "preview_host"),
			strVal(result, "status"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, result)
}

func runPreviewList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var previews []map[string]any
	if err := client.GetJSON(ctx, "/api/previews", &previews); err != nil {
		return fmt.Errorf("list previews: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "json" {
		return cli.PrintJSON(os.Stdout, previews)
	}

	headers := []string{"ID", "APP", "NAMESPACE", "HOST", "STATUS"}
	rows := make([][]string, 0, len(previews))
	for _, p := range previews {
		rows = append(rows, []string{
			strVal(p, "id"),
			strVal(p, "app"),
			strVal(p, "namespace_name"),
			strVal(p, "preview_host"),
			strVal(p, "status"),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runPreviewGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var preview map[string]any
	if err := client.GetJSON(ctx, "/api/previews/"+args[0], &preview); err != nil {
		return fmt.Errorf("get preview: %w", err)
	}

	output, _ := cmd.Flags().GetString("output")
	if output == "table" {
		headers := []string{"ID", "APP", "NAMESPACE", "HOST", "STATUS"}
		rows := [][]string{{
			strVal(preview, "id"),
			strVal(preview, "app"),
			strVal(preview, "namespace_name"),
			strVal(preview, "preview_host"),
			strVal(preview, "status"),
		}}
		cli.PrintTable(os.Stdout, headers, rows)
		return nil
	}

	return cli.PrintJSON(os.Stdout, preview)
}

func runPreviewDelete(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := client.DeleteJSON(ctx, "/api/previews/"+args[0]); err != nil {
		return fmt.Errorf("delete preview: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Preview %s deleted.\n", args[0])
	return nil
}
