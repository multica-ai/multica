package execenv

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOpenclawIsolationPermissionsReuseAndMCP(t *testing.T) {
	root := t.TempDir()
	rows := []any{map[string]any{openclawEntriesKeyField: "main", "skills": []any{"existing"}}}
	mcp := map[string]any{"servers": map[string]any{"fixture": map[string]any{"url": "https://example.invalid/mcp", "headers": map[string]any{"Authorization": "fixture-only"}}}}
	cfg := buildPerTaskOpenclawConfig("/shared/config", true, "", rows, openclawAgentsSourceEntries, filepath.Join(root, "workdir"), nil, false, OpenclawGatewayPin{})
	cfg["mcp"] = mcp
	if err := prepareOpenclawIsolation(cfg, root, rows, openclawAgentsSourceEntries, []string{"existing", "task"}); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "openclaw-state")
	info, err := os.Stat(state)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("private state: %v %v", info, err)
	}
	marker := filepath.Join(state, "session-marker")
	if err := os.WriteFile(marker, []byte("retained"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareOpenclawIsolation(cfg, root, rows, openclawAgentsSourceEntries, []string{"existing", "task"}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "retained" {
		t.Fatal("reuse lost session state")
	}
	if !reflect.DeepEqual(cfg["mcp"], mcp) {
		t.Fatal("MCP definitions changed")
	}
	if !reflect.DeepEqual(cfg["$include"], []any{"/shared/config"}) {
		t.Fatalf("credential include changed: %v", cfg["$include"])
	}
	entries := cfg["agents"].(map[string]any)["entries"].(map[string]any)
	if !reflect.DeepEqual(entries["main"].(map[string]any)["skills"], []any{"existing", "task"}) {
		t.Fatal("duplicate skill merge")
	}
	other := t.TempDir()
	otherCfg := buildPerTaskOpenclawConfig("/shared/config", true, "", rows, openclawAgentsSourceEntries, filepath.Join(other, "workdir"), nil, false, OpenclawGatewayPin{})
	if err := prepareOpenclawIsolation(otherCfg, other, rows, openclawAgentsSourceEntries, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(other, "openclaw-state", "session-marker")); !os.IsNotExist(err) {
		t.Fatal("task state crossed roots")
	}
}

func TestOpenclawIsolationRejectsSymlinkAndTraversal(t *testing.T) {
	root := t.TempDir()
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(root, "openclaw-state")); err != nil {
		t.Fatal(err)
	}
	if err := prepareOpenclawIsolation(map[string]any{}, root, nil, openclawAgentsSourceEntries, nil); err == nil {
		t.Fatal("accepted shared state symlink")
	}
	for _, id := range []string{"..", "../shared", "a/b", "a\\b", ""} {
		rows := []any{map[string]any{openclawEntriesKeyField: id}}
		cfg := map[string]any{"agents": map[string]any{"entries": map[string]any{id: map[string]any{}}}}
		if err := prepareOpenclawIsolation(cfg, t.TempDir(), rows, openclawAgentsSourceEntries, nil); err == nil {
			t.Fatalf("accepted id %q", id)
		}
	}
}
