package main

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// ---------------------------------------------------------------------------
// Command tree
// ---------------------------------------------------------------------------

var pluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Manage Multica plugins",
}

var pluginInstallCmd = &cobra.Command{
	Use:   "install <name>",
	Short: "Publish and install a built-in plugin by name (e.g. cerebra)",
	Args:  exactArgs(1),
	RunE:  runPluginInstall,
}

var pluginListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed plugins in the workspace",
	RunE:  runPluginList,
}

var pluginUninstallCmd = &cobra.Command{
	Use:   "uninstall <installation-id>",
	Short: "Uninstall a plugin from the workspace",
	Args:  exactArgs(1),
	RunE:  runPluginUninstall,
}

var pluginPublishCmd = &cobra.Command{
	Use:   "publish <path-to-zip>",
	Short: "Publish a plugin package (.zip) to the workspace",
	Args:  exactArgs(1),
	RunE:  runPluginPublish,
}

var installCmd = &cobra.Command{
	Use:   "install <name>",
	Short: "Install a plugin by name (e.g. cerebra)",
	Args:  exactArgs(1),
	RunE:  runPluginInstall,
}

func init() {
	pluginCmd.AddCommand(pluginInstallCmd)
	pluginCmd.AddCommand(pluginListCmd)
	pluginCmd.AddCommand(pluginUninstallCmd)
	pluginCmd.AddCommand(pluginPublishCmd)
}

// ---------------------------------------------------------------------------
// Built-in plugin registry
// ---------------------------------------------------------------------------

// builtinPlugin describes a plugin bundled with the Multica installation.
type builtinPlugin struct {
	key           string   // com.multica.cerebra
	manifestPath  string   // relative path within MULTICA_PLUGIN_DIR
	grantedScopes []string // auto-granted (no consent screen in CLI install)
}

// builtinPlugins maps the short install name (e.g. "cerebra") to its descriptor.
// The manifest path is relative to MULTICA_PLUGIN_DIR (./plugins by default).
var builtinPlugins = map[string]builtinPlugin{
	"cerebra": {
		key:          "com.multica.cerebra",
		manifestPath: "cerebra",
		grantedScopes: []string{
			"issues:read",
			"tasks:read",
			"storage:workspace",
			"net:cerebra.localhost",
		},
	},
}

// ---------------------------------------------------------------------------
// multica plugin install <name>
// ---------------------------------------------------------------------------

func runPluginInstall(cmd *cobra.Command, args []string) error {
	name := strings.ToLower(strings.TrimSpace(args[0]))

	plugin, ok := builtinPlugins[name]
	if !ok {
		return fmt.Errorf("unknown plugin %q — available built-in plugins: %s", name, knownPluginNames())
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}

	workspaceID := getRequiredWorkspaceID(cmd)
	if workspaceID == "" {
		return fmt.Errorf("workspace ID required — set MULTICA_WORKSPACE_ID or use --workspace-id")
	}

	ctx := context.Background()

	fmt.Printf("📦 Publishing plugin %q...\n", name)

	// Step 1: bundle and publish the plugin zip from the local plugin dir
	pluginDir := resolvePluginDir()
	versionID, err := publishBuiltinPlugin(ctx, client, workspaceID, pluginDir, plugin)
	if err != nil {
		return fmt.Errorf("publish failed: %w", err)
	}

	fmt.Printf("✅ Published: version %s\n", versionID)
	fmt.Printf("🔌 Installing plugin %q...\n", name)

	// Step 2: install from the published version
	var result map[string]any
	body := map[string]any{
		"version_id":     versionID,
		"granted_scopes": plugin.grantedScopes,
	}
	if err := client.PostJSON(ctx, "/api/workspaces/"+workspaceID+"/plugins", body, &result); err != nil {
		return fmt.Errorf("install failed: %w", err)
	}

	installID, _ := result["id"].(string)
	fmt.Printf("✅ Cerebra is installed (id: %s)\n", installID)
	fmt.Println("🧠 All agents now automatically use intelligent model routing.")
	fmt.Println("   Simple tasks → lightweight model")
	fmt.Println("   Debug/code tasks → standard model")
	fmt.Println("   Architecture tasks → frontier model")

	return nil
}

// publishBuiltinPlugin zips the plugin directory and uploads it to the API.
// Returns the published version_id.
func publishBuiltinPlugin(ctx context.Context, client *cli.APIClient, workspaceID, pluginDir string, plugin builtinPlugin) (string, error) {
	dir := filepath.Join(pluginDir, plugin.manifestPath)
	if _, err := os.Stat(dir); err != nil {
		return "", fmt.Errorf("plugin directory not found at %s: %w", dir, err)
	}

	// Build the zip bundle in memory — only manifest + skills, no binary
	archive, err := buildPluginZip(dir)
	if err != nil {
		return "", fmt.Errorf("build zip: %w", err)
	}

	var published map[string]any
	filename := plugin.manifestPath + ".zip"
	if err := client.UploadPluginPackage(ctx, workspaceID, archive, filename, &published); err != nil {
		if strings.Contains(err.Error(), "already published") || strings.Contains(err.Error(), "409") {
			existingVerID, findErr := findExistingVersionID(ctx, client, workspaceID, plugin.key)
			if findErr == nil && existingVerID != "" {
				return existingVerID, nil
			}
		}
		return "", err
	}

	versionID, _ := published["version_id"].(string)
	if versionID == "" {
		if versions, ok := published["versions"].([]any); ok && len(versions) > 0 {
			if v, ok := versions[0].(map[string]any); ok {
				versionID, _ = v["id"].(string)
			}
		}
	}
	if versionID == "" {
		return "", fmt.Errorf("server returned no version_id")
	}
	return versionID, nil
}

func findExistingVersionID(ctx context.Context, client *cli.APIClient, workspaceID, pluginKey string) (string, error) {
	var result map[string]any
	if err := client.GetJSON(ctx, "/api/workspaces/"+workspaceID+"/plugins/packages", &result); err != nil {
		return "", err
	}
	pkgs, _ := result["packages"].([]any)
	for _, p := range pkgs {
		pkgMap, _ := p.(map[string]any)
		if pkgMap["plugin_key"] == pluginKey {
			if versions, ok := pkgMap["versions"].([]any); ok && len(versions) > 0 {
				if v, ok := versions[0].(map[string]any); ok {
					if id, ok := v["id"].(string); ok {
						return id, nil
					}
				}
			}
		}
	}
	return "", fmt.Errorf("version not found for %s", pluginKey)
}

// buildPluginZip creates an in-memory zip containing only the files the plugin
// manifest references (manifest + skills). Excludes binaries to stay under
// the 2MB plugincontract.MaxBundleSize limit.
func buildPluginZip(dir string) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		// Skip binaries — only manifest + markdown skill files
		ext := strings.ToLower(filepath.Ext(path))
		base := strings.ToLower(filepath.Base(path))
		if ext == "" && base != "multica.plugin.json" {
			return nil // skip executables (no extension on Unix)
		}
		if ext == ".exe" || ext == ".so" || ext == ".dylib" {
			return nil
		}

		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		// zip entries always use forward slashes
		rel = filepath.ToSlash(rel)

		w, err := zw.Create(rel)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// multica plugin list
// ---------------------------------------------------------------------------

func runPluginList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	workspaceID := getRequiredWorkspaceID(cmd)
	if workspaceID == "" {
		return fmt.Errorf("workspace ID required — set MULTICA_WORKSPACE_ID or use --workspace-id")
	}

	var result map[string]any
	if err := client.GetJSON(context.Background(), "/api/workspaces/"+workspaceID+"/plugins", &result); err != nil {
		return err
	}

	plugins, _ := result["plugins"].([]any)
	if len(plugins) == 0 {
		fmt.Println("No plugins installed.")
		return nil
	}
	fmt.Printf("%-36s  %-30s  %-8s  %s\n", "ID", "Name", "Enabled", "Version")
	fmt.Println(strings.Repeat("-", 90))
	for _, p := range plugins {
		m, _ := p.(map[string]any)
		id, _ := m["id"].(string)
		enabled, _ := m["enabled"].(bool)
		name, _ := m["name"].(string)
		version, _ := m["version"].(string)
		if name == "" {
			manifest, _ := m["manifest"].(map[string]any)
			if manifest != nil {
				name, _ = manifest["name"].(string)
				version, _ = manifest["version"].(string)
			}
		}
		status := "no"
		if enabled {
			status = "yes"
		}
		fmt.Printf("%-36s  %-30s  %-8s  %s\n", id, name, status, version)
	}
	return nil
}

// ---------------------------------------------------------------------------
// multica plugin uninstall <installation-id>
// ---------------------------------------------------------------------------

func runPluginUninstall(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	workspaceID := getRequiredWorkspaceID(cmd)
	if workspaceID == "" {
		return fmt.Errorf("workspace ID required — set MULTICA_WORKSPACE_ID or use --workspace-id")
	}

	path := "/api/workspaces/" + workspaceID + "/plugins/" + args[0]
	if err := client.DeleteJSON(context.Background(), path); err != nil {
		return err
	}
	fmt.Printf("✅ Plugin %s uninstalled.\n", args[0])
	return nil
}

// ---------------------------------------------------------------------------
// multica plugin publish <path-to-zip>
// ---------------------------------------------------------------------------

func runPluginPublish(cmd *cobra.Command, args []string) error {
	zipPath := args[0]
	data, err := os.ReadFile(zipPath)
	if err != nil {
		return fmt.Errorf("read zip: %w", err)
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	workspaceID := getRequiredWorkspaceID(cmd)
	if workspaceID == "" {
		return fmt.Errorf("workspace ID required — set MULTICA_WORKSPACE_ID or use --workspace-id")
	}
	ctx := context.Background()

	var result map[string]any
	if err := client.UploadPluginPackage(ctx, workspaceID, data, filepath.Base(zipPath), &result); err != nil {
		return err
	}

	fmt.Printf("✅ Published: %v\n", result["version_id"])
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func getRequiredWorkspaceID(cmd *cobra.Command) string {
	return resolveWorkspaceID(cmd)
}

func resolvePluginDir() string {
	if d := os.Getenv("MULTICA_PLUGIN_DIR"); d != "" {
		return d
	}
	cwd, _ := os.Getwd()
	candidates := []string{
		filepath.Join(cwd, "plugins"),
		filepath.Join(cwd, "..", "plugins"),
		filepath.Join(cwd, "..", "..", "plugins"),
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	return filepath.Join(cwd, "plugins")
}

func knownPluginNames() string {
	names := make([]string, 0, len(builtinPlugins))
	for k := range builtinPlugins {
		names = append(names, k)
	}
	return strings.Join(names, ", ")
}
