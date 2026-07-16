// CEREBRO-PATCH(cerebro-mini-apps-cli): FIR-3172 app catalog lifecycle.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
var appDisableCmd = &cobra.Command{Use: "disable <app-id>", Short: "Disable a published app", Args: exactArgs(1), RunE: runAppDisable}

func init() {
	appListCmd.Flags().String("output", "table", "Output format: table or json")
	appCreateCmd.Flags().String("name", "", "App name")
	appCreateCmd.Flags().String("slug", "", "Stable app slug")
	appCreateCmd.Flags().String("description", "", "App description")
	appPreviewCmd.Flags().String("file", "", "App snapshot JSON file")
	appPublishCmd.Flags().String("dir", "", "App bundle directory")
	appPublishCmd.Flags().String("version", "", "Semantic version to publish")
	appPublishCmd.Flags().String("release-notes", "", "Required release notes")
	appRollbackCmd.Flags().String("version", "", "Published version to restore")
	for _, command := range []*cobra.Command{appCreateCmd, appPreviewCmd, appPublishCmd, appRollbackCmd, appDisableCmd} {
		command.Flags().String("output", "json", "Output format: json")
	}
	appCmd.AddCommand(appCreateCmd, appPreviewCmd, appPublishCmd, appRollbackCmd, appDisableCmd, appListCmd)
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
	dir, _ := cmd.Flags().GetString("dir")
	if dir == "" {
		return fmt.Errorf("--dir is required")
	}
	files, err := readAppBundle(dir)
	if err != nil {
		return err
	}
	body := map[string]any{"files": files}
	version, _ := cmd.Flags().GetString("version")
	releaseNotes, _ := cmd.Flags().GetString("release-notes")
	if version == "" || releaseNotes == "" {
		return fmt.Errorf("--version and --release-notes are required")
	}
	body["version"] = version
	body["release_notes"] = releaseNotes
	return appPost(cmd, "/api/cerebro/apps/"+url.PathEscape(args[0])+"/publish", body)
}

func runAppDisable(cmd *cobra.Command, args []string) error {
	return appPost(cmd, "/api/cerebro/apps/"+url.PathEscape(args[0])+"/disable", map[string]any{})
}

func readAppBundle(root string) ([]map[string]any, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	files := make([]map[string]any, 0)
	total := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root || entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("app bundle may contain regular files only")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative != "app.json" && !strings.HasPrefix(relative, "frontend/") && !strings.HasPrefix(relative, "backend/") {
			return fmt.Errorf("app bundle path %q is outside app.json, frontend, and backend", relative)
		}
		if len(files) >= 100 || info.Size() > 512<<10 || total+int(info.Size()) > 5<<20 {
			return fmt.Errorf("app bundle exceeds its file or size limit")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		total += len(content)
		hash := sha256.Sum256(content)
		mediaType := mime.TypeByExtension(filepath.Ext(relative))
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		files = append(files, map[string]any{
			"path":           relative,
			"media_type":     mediaType,
			"content_base64": base64.StdEncoding.EncodeToString(content),
			"sha256":         fmt.Sprintf("%x", hash),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("app bundle is empty")
	}
	return files, nil
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
