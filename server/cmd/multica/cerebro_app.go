// CEREBRO-PATCH(cerebro-mini-apps-cli): FIR-3172 app catalog lifecycle.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var appCmd = &cobra.Command{Use: "app", Short: "Build and publish workspace apps"}

var appListCmd = &cobra.Command{Use: "list", Short: "List apps", Args: cobra.NoArgs, RunE: runAppList}
var appCreateCmd = &cobra.Command{Use: "create", Short: "Create an app draft", Args: cobra.NoArgs, RunE: runAppCreate}
var appPreviewCmd = &cobra.Command{Use: "preview <app-id>", Short: "Create an app preview", Args: exactArgs(1), RunE: runAppPreview}
var appPublishCmd = &cobra.Command{Use: "publish <app-id>", Short: "Publish an app version", Args: exactArgs(1), RunE: runAppPublish}
var appRollbackCmd = &cobra.Command{Use: "rollback <app-id>", Short: "Roll an app back to a published version", Args: exactArgs(1), RunE: runAppRollback}

func init() {
	appListCmd.Flags().String("output", "table", "Output format: table or json")
	appCreateCmd.Flags().String("name", "", "App name")
	appCreateCmd.Flags().String("slug", "", "Stable app slug")
	appCreateCmd.Flags().String("description", "", "App description")
	appPreviewCmd.Flags().String("file", "", "App snapshot JSON file")
	appPublishCmd.Flags().String("file", "", "App snapshot JSON file")
	appPublishCmd.Flags().String("version", "", "Semantic version to publish")
	appPublishCmd.Flags().String("release-notes", "", "Required release notes")
	appRollbackCmd.Flags().String("version", "", "Published version to restore")
	for _, command := range []*cobra.Command{appCreateCmd, appPreviewCmd, appPublishCmd, appRollbackCmd} {
		command.Flags().String("output", "json", "Output format: json")
	}
	appCmd.AddCommand(appCreateCmd, appPreviewCmd, appPublishCmd, appRollbackCmd, appListCmd)
	appCmd.AddCommand(appWorkflowCmd)
}

func appClient(cmd *cobra.Command) (*cli.APIClient, context.Context, context.CancelFunc, error) {
	client, err := newAPIClient(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	return client, ctx, cancel, nil
}

func runAppList(cmd *cobra.Command, _ []string) error {
	client, ctx, cancel, err := appClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out struct {
		Apps []map[string]any `json:"apps"`
	}
	if err := client.GetJSON(ctx, "/api/cerebro/apps", &out); err != nil {
		return err
	}
	format, _ := cmd.Flags().GetString("output")
	if format == "json" {
		return cli.PrintJSON(os.Stdout, out)
	}
	rows := make([][]string, 0, len(out.Apps))
	for _, item := range out.Apps {
		rows = append(rows, []string{fmt.Sprint(item["id"]), fmt.Sprint(item["name"]), fmt.Sprint(item["current_version"]), fmt.Sprint(item["status"])})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "NAME", "VERSION", "STATUS"}, rows)
	return nil
}

func runAppCreate(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	slug, _ := cmd.Flags().GetString("slug")
	description, _ := cmd.Flags().GetString("description")
	if name == "" || slug == "" {
		return fmt.Errorf("--name and --slug are required")
	}
	return appPost(cmd, "/api/cerebro/apps", map[string]any{"name": name, "slug": slug, "description": description})
}

func runAppPreview(cmd *cobra.Command, args []string) error {
	body, err := appSnapshotBody(cmd)
	if err != nil {
		return err
	}
	return appPost(cmd, "/api/cerebro/apps/"+url.PathEscape(args[0])+"/preview", body)
}

func runAppPublish(cmd *cobra.Command, args []string) error {
	body, err := appSnapshotBody(cmd)
	if err != nil {
		return err
	}
	version, _ := cmd.Flags().GetString("version")
	releaseNotes, _ := cmd.Flags().GetString("release-notes")
	if version == "" || releaseNotes == "" {
		return fmt.Errorf("--version and --release-notes are required")
	}
	body["version"] = version
	body["release_notes"] = releaseNotes
	return appPost(cmd, "/api/cerebro/apps/"+url.PathEscape(args[0])+"/publish", body)
}

func runAppRollback(cmd *cobra.Command, args []string) error {
	version, _ := cmd.Flags().GetString("version")
	if version == "" {
		return fmt.Errorf("--version is required")
	}
	return appPost(cmd, "/api/cerebro/apps/"+url.PathEscape(args[0])+"/rollback", map[string]any{"version": version})
}

func appSnapshotBody(cmd *cobra.Command) (map[string]any, error) {
	path, _ := cmd.Flags().GetString("file")
	if path == "" {
		return nil, fmt.Errorf("--file is required")
	}
	return readJSONFile(path)
}

func readJSONFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("invalid app JSON: %w", err)
	}
	return body, nil
}

func appPost(cmd *cobra.Command, path string, body map[string]any) error {
	client, ctx, cancel, err := appClient(cmd)
	if err != nil {
		return err
	}
	defer cancel()
	var out any
	if err := client.PostJSON(ctx, path, body, &out); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, out)
}
