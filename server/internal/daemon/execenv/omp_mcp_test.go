package execenv

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareOmpMcpConfigUsesProviderNamespace(t *testing.T) {
	workDir := t.TempDir()
	manifest := &sidecarManifest{}
	raw := json.RawMessage(`{"mcpServers":{"fetch":{"command":"uvx"}}}`)

	if err := prepareOmpMcpConfig(workDir, "omp", raw, manifest); err != nil {
		t.Fatalf("prepareOmpMcpConfig: %v", err)
	}
	path := filepath.Join(workDir, ".omp", "mcp.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(data) != string(raw) {
		t.Fatalf("config = %s, want %s", data, raw)
	}
	if !containsPath(manifest.Files, path) || !containsPath(manifest.Dirs, filepath.Dir(path)) {
		t.Fatalf("manifest = %#v, want config file and directory", manifest)
	}
}

func TestPrepareOmpMcpConfigRefusesExistingManagedPath(t *testing.T) {
	workDir := t.TempDir()
	path := filepath.Join(workDir, ".omp", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := []byte(`{"mcpServers":{"user":{"command":"echo"}}}`)
	if err := os.WriteFile(path, existing, 0o644); err != nil {
		t.Fatal(err)
	}

	err := prepareOmpMcpConfig(workDir, "omp", json.RawMessage(`{"mcpServers":{"managed":{}}}`), &sidecarManifest{})
	if err == nil || !strings.Contains(err.Error(), "would overwrite") {
		t.Fatalf("error = %v, want overwrite error", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != string(existing) {
		t.Fatalf("existing config changed to %s", data)
	}
}

func TestPrepareOmpMcpConfigCleanupRemovesManagedFiles(t *testing.T) {
	workDir := t.TempDir()
	envRoot := t.TempDir()
	manifest := &sidecarManifest{}
	if err := prepareOmpMcpConfig(workDir, "omp", json.RawMessage(`{"mcpServers":{"fetch":{}}}`), manifest); err != nil {
		t.Fatal(err)
	}
	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSidecars(envRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".omp", "mcp.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("managed config still exists, stat error = %v", err)
	}
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}
