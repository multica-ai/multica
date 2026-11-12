package execenv

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestOpenclawIsolatedConfig(t *testing.T) {
	root := t.TempDir()
	rows := []any{
		map[string]any{openclawEntriesKeyField: "main", "skills": []any{"existing"}, "agentDir": "/shared/agent", "model": "untouched"},
		map[string]any{openclawEntriesKeyField: "other"},
	}
	cfg := buildPerTaskOpenclawConfig("/shared/config", true, "", rows, openclawAgentsSourceEntries, filepath.Join(root, "workdir"), nil, false, OpenclawGatewayPin{})
	if err := prepareOpenclawIsolation(cfg, root, rows, openclawAgentsSourceEntries, []string{"task-skill"}); err != nil {
		t.Fatal(err)
	}
	entries := cfg["agents"].(map[string]any)["entries"].(map[string]any)
	main := entries["main"].(map[string]any)
	if !reflect.DeepEqual(main["skills"], []any{"existing", "task-skill"}) {
		t.Fatalf("skills=%v", main["skills"])
	}
	if main["agentDir"] != filepath.Join(root, "openclaw-state", "agents", "main", "agent") {
		t.Fatalf("agentDir=%v", main["agentDir"])
	}
	if _, ok := main["model"]; ok {
		t.Fatal("must not copy model/auth fields")
	}
	if _, ok := entries["other"].(map[string]any)["skills"]; ok {
		t.Fatal("unrestricted skills must remain unrestricted")
	}
	if rows[0].(map[string]any)["agentDir"] != "/shared/agent" {
		t.Fatal("mutated discovery input")
	}
}

func TestOpenclawIsolationRejectsUnsupportedRegistry(t *testing.T) {
	if err := prepareOpenclawIsolation(map[string]any{}, t.TempDir(), nil, openclawAgentsSourceRegistry, nil); err == nil {
		t.Fatal("registry-only configuration cannot guarantee isolation")
	}
}
