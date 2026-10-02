package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestConvertDisabledRuntimeSkillsDefersHostPluginResolution(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	install := filepath.Join(home, "plugin-install")
	registry, err := json.Marshal(map[string]any{"plugins": map[string]any{
		"paper@market": []map[string]string{{"scope": "user", "installPath": install}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for path, raw := range map[string][]byte{
		filepath.Join(home, ".claude", "settings.json"):                     []byte(`{"enabledPlugins":{"paper@market":true}}`),
		filepath.Join(home, ".claude", "plugins", "installed_plugins.json"): registry,
		filepath.Join(install, ".claude-plugin", "plugin.json"):             []byte(`{"name":"paper"}`),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	agent := &AgentData{DisabledRuntimeSkills: []DisabledRuntimeSkillData{
		{RuntimeID: "runtime", Provider: "claude", Root: "plugin", Key: "paper:hidden", Plugin: "paper@market"},
		{RuntimeID: "other", Provider: "claude", Root: "plugin", Key: "paper:visible", Plugin: "paper@market"},
	}}
	refs := convertDisabledRuntimeSkillsForEnv(agent, "runtime", "claude")
	if len(refs) != 1 || refs[0].PluginPath != "" || refs[0].Key != "paper:hidden" {
		t.Fatalf("conversion must not select a globally enabled install before the task cwd exists: %+v", refs)
	}
}
