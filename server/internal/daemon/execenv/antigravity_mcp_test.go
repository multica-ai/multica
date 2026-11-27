package execenv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestPrepareAntigravityMcpConfig(t *testing.T) {
	workDir := t.TempDir()
	manifest := &sidecarManifest{}
	raw := json.RawMessage(`{"mcpServers":{"local":{"command":"node","args":["server.js"],"env":{"TOKEN":"test-secret"}},"remote":{"url":"https://example.test/mcp","headers":{"Authorization":"Bearer test-secret"}},"legacy":{"serverUrl":"https://example.test/other"}}}`)
	if err := prepareAntigravityMcpConfig(workDir, "antigravity", raw, manifest); err != nil {
		t.Fatal(err)
	}
	pluginDir := filepath.Join(workDir, ".agents", "plugins", "multica")
	var plugin map[string]string
	readAntigravityJSON(t, filepath.Join(pluginDir, "plugin.json"), &plugin)
	if plugin["name"] != "multica" || plugin["description"] == "" {
		t.Fatalf("plugin = %v", plugin)
	}
	var config struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	readAntigravityJSON(t, filepath.Join(pluginDir, "mcp_config.json"), &config)
	var original struct {
		Servers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.Servers["local"], original.Servers["local"]) {
		t.Fatal("stdio fields changed")
	}
	for _, name := range []string{"remote", "legacy"} {
		server := config.Servers[name]
		if server["url"] == nil || server["url"] != server["serverUrl"] {
			t.Fatalf("%s URL aliases do not match", name)
		}
	}
	if !reflect.DeepEqual(config.Servers["remote"]["headers"], original.Servers["remote"]["headers"]) {
		t.Fatal("HTTP headers changed")
	}
	for _, name := range []string{"plugin.json", "mcp_config.json"} {
		path := filepath.Join(pluginDir, name)
		if !containsPath(manifest.Files, path) {
			t.Fatalf("untracked file %s", path)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o", name, info.Mode().Perm())
		}
	}
	if !containsPath(manifest.Dirs, pluginDir) {
		t.Fatal("plugin directory is untracked")
	}
}

func TestPrepareAntigravityMcpConfigNoOp(t *testing.T) {
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(" \n null ")} {
		if err := prepareAntigravityMcpConfig("", "antigravity", raw, &sidecarManifest{}); err != nil {
			t.Fatal(err)
		}
	}
	workDir := t.TempDir()
	if err := prepareAntigravityMcpConfig(workDir, "claude", json.RawMessage(`invalid`), &sidecarManifest{}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(workDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unexpected writes: %v, %v", entries, err)
	}
	if err := prepareAntigravityMcpConfig("", "antigravity", json.RawMessage(`{}`), &sidecarManifest{}); err == nil {
		t.Fatal("missing workdir accepted")
	}
}

func TestPrepareAntigravityMcpConfigEmptyManagedSet(t *testing.T) {
	for _, raw := range []string{`{}`, `{"mcpServers":{}}`} {
		workDir := t.TempDir()
		if err := prepareAntigravityMcpConfig(workDir, "antigravity", json.RawMessage(raw), &sidecarManifest{}); err != nil {
			t.Fatal(err)
		}
		var config map[string]map[string]any
		readAntigravityJSON(t, filepath.Join(workDir, ".agents", "plugins", "multica", "mcp_config.json"), &config)
		if config["mcpServers"] == nil || len(config["mcpServers"]) != 0 {
			t.Fatal("expected explicit empty server set")
		}
	}
}

func TestPrepareAntigravityMcpConfigRejectsInvalidConfig(t *testing.T) {
	for _, raw := range []string{
		`invalid`, `[]`, `{"mcpServers":[]}`, `{"mcpServers":{" ":{}}}`,
		`{"mcpServers":{"bad":null}}`, `{"mcpServers":{"bad":42}}`,
		`{"mcpServers":{"bad":{}}}`, `{"mcpServers":{"bad":{"command":1}}}`,
		`{"mcpServers":{"bad":{"url":1}}}`, `{"mcpServers":{"bad":{"url":" "}}}`,
		`{"mcpServers":{"bad":{"url":"https://one.test","serverUrl":"https://two.test"}}}`,
		`{"mcpServers":{"bad":{"command":"node","url":"https://one.test"}}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			workDir := t.TempDir()
			manifest := &sidecarManifest{}
			if err := prepareAntigravityMcpConfig(workDir, "antigravity", json.RawMessage(raw), manifest); err == nil {
				t.Fatal("invalid config accepted")
			}
			entries, err := os.ReadDir(workDir)
			if err != nil || len(entries) != 0 || len(manifest.Files) != 0 {
				t.Fatalf("invalid input left sidecars: %v, %v", entries, err)
			}
		})
	}
}

func TestPrepareAntigravityMcpConfigRefusesExistingPlugin(t *testing.T) {
	for _, name := range []string{"plugin.json", "mcp_config.json", "user.txt"} {
		t.Run(name, func(t *testing.T) {
			workDir := t.TempDir()
			pluginDir := filepath.Join(workDir, ".agents", "plugins", "multica")
			if err := os.MkdirAll(pluginDir, 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(pluginDir, name)
			if err := os.WriteFile(path, []byte("user content"), 0o644); err != nil {
				t.Fatal(err)
			}
			err := prepareAntigravityMcpConfig(workDir, "antigravity", json.RawMessage(`{}`), &sidecarManifest{})
			if err == nil || !strings.Contains(err.Error(), "would overwrite") {
				t.Fatalf("expected conflict, got %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != "user content" {
				t.Fatal("user content changed")
			}
			entries, err := os.ReadDir(pluginDir)
			if err != nil || len(entries) != 1 {
				t.Fatal("conflict left managed files")
			}
		})
	}
}

func TestPrepareAntigravityMcpConfigRejectsSymlinkParents(t *testing.T) {
	for _, relative := range []string{".agents", filepath.Join(".agents", "plugins"), filepath.Join(".agents", "plugins", "multica")} {
		t.Run(relative, func(t *testing.T) {
			workDir, outside := t.TempDir(), t.TempDir()
			path := filepath.Join(workDir, relative)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := prepareAntigravityMcpConfig(workDir, "antigravity", json.RawMessage(`{}`), &sidecarManifest{}); err == nil {
				t.Fatal("symlink accepted")
			}
			entries, err := os.ReadDir(outside)
			if err != nil || len(entries) != 0 {
				t.Fatal("wrote outside workdir")
			}
		})
	}
}

func TestPrepareAntigravityMcpConfigCleanup(t *testing.T) {
	workDir, envRoot := t.TempDir(), t.TempDir()
	userDir := filepath.Join(workDir, ".agents", "plugins", "user")
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := &sidecarManifest{}
	if err := prepareAntigravityMcpConfig(workDir, "antigravity", json.RawMessage(`{}`), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSidecars(envRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".agents", "plugins", "multica")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed plugin remains: %v", err)
	}
	if _, err := os.Stat(userDir); err != nil {
		t.Fatalf("user plugin removed: %v", err)
	}
}

func TestPrepareAntigravityMcpLifecycle(t *testing.T) {
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "ws", TaskID: "task", Provider: "antigravity",
		McpConfig: json.RawMessage(`{"mcpServers":{"first":{"command":"first"}}}`),
	}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.Cleanup(true) })
	path := filepath.Join(env.WorkDir, ".agents", "plugins", "multica", "mcp_config.json")
	var config map[string]map[string]any
	readAntigravityJSON(t, path, &config)
	if config["mcpServers"]["first"] == nil {
		t.Fatal("Prepare did not wire MCP")
	}
	for _, raw := range []json.RawMessage{json.RawMessage(`{"mcpServers":{"second":{"command":"second"}}}`), nil} {
		reused := Reuse(ReuseParams{WorkDir: env.WorkDir, Provider: "antigravity", McpConfig: raw}, testLogger())
		if reused == nil {
			t.Fatal("Reuse failed")
		}
		if raw == nil {
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("stale config after removing managed MCP: %v", err)
			}
		} else {
			readAntigravityJSON(t, path, &config)
			if config["mcpServers"]["first"] != nil || config["mcpServers"]["second"] == nil {
				t.Fatal("Reuse did not replace server set")
			}
		}
	}
}

func TestPrepareAntigravityMcpFailurePreservesLocalFiles(t *testing.T) {
	workDir := t.TempDir()
	pluginDir := filepath.Join(workDir, ".agents", "plugins", "multica")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Prepare(PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "ws", TaskID: "task", Provider: "antigravity", LocalWorkDir: workDir, McpConfig: json.RawMessage(`{}`)}, testLogger())
	if err == nil {
		t.Fatal("Prepare accepted a conflicting plugin")
	}
	if _, err := os.Stat(pluginDir); err != nil {
		t.Fatal("removed user's directory")
	}
	if _, err := os.Stat(filepath.Join(workDir, TaskContextMarkerRelPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed Prepare left context files")
	}
}

func TestReuseAntigravityMcpConflictRollsBackContext(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "workdir")
	pluginDir := filepath.Join(workDir, ".agents", "plugins", "multica")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	env := Reuse(ReuseParams{WorkDir: workDir, Provider: "antigravity", McpConfig: json.RawMessage(`{}`)}, testLogger())
	if env != nil {
		t.Fatal("Reuse accepted a conflicting plugin")
	}
	if _, err := os.Stat(pluginDir); err != nil {
		t.Fatal("removed user's plugin directory")
	}
	if _, err := os.Stat(filepath.Join(workDir, TaskContextMarkerRelPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed Reuse left untracked context files")
	}
}

func readAntigravityJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal(err)
	}
}
