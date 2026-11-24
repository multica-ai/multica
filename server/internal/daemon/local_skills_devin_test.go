package daemon

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestDevinLocalSkillRoots(t *testing.T) {
	home := t.TempDir()
	custom := filepath.Join(home, "custom-config")
	for _, tc := range []struct {
		name, goos, xdg, appData, want string
	}{
		{"unix-default", "linux", "", "", filepath.Join(home, ".config")},
		{"unix-xdg", "darwin", custom, "", custom},
		{"windows-appdata", "windows", "ignored", custom, custom},
		{"windows-fallback", "windows", "ignored", "", filepath.Join(home, "AppData", "Roaming")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			primary, legacy := devinLocalSkillRoots(home, tc.goos, tc.xdg, tc.appData)
			if primary != filepath.Join(tc.want, "devin", "skills") || !reflect.DeepEqual(legacy, []string{
				filepath.Join(tc.want, "cognition", "skills"),
				filepath.Join(home, ".codeium", "windsurf", "skills"),
				filepath.Join(home, ".codeium", "windsurf-next", "skills"),
				filepath.Join(home, ".codeium", "windsurf-insiders", "skills"),
			}) {
				t.Fatalf("roots = %q %q", primary, legacy)
			}
		})
	}
}

func TestDevinLocalSkillsMergeAndImportPrecedence(t *testing.T) {
	home := t.TempDir()
	setTestUserHome(t, home)
	configDir := filepath.Join(home, "config")
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("APPDATA", configDir)
	roots := []string{
		filepath.Join(configDir, "devin", "skills"), filepath.Join(configDir, "cognition", "skills"),
		filepath.Join(home, ".codeium", "windsurf", "skills"), filepath.Join(home, ".codeium", "windsurf-next", "skills"),
		filepath.Join(home, ".codeium", "windsurf-insiders", "skills"), filepath.Join(home, ".agents", "skills"),
	}
	names := []string{"Devin Copy", "Cognition Copy", "Windsurf Copy", "Next Copy", "Insiders Copy", "Universal Copy"}
	keys := []string{"devin-only", "legacy-only", "windsurf-only", "next-only", "insiders-only", "universal-only"}
	for i, root := range roots {
		writeTestLocalSkill(t, root, "duplicate", map[string]string{"SKILL.md": "---\nname: " + names[i] + "\n---\n"})
		writeTestLocalSkill(t, root, keys[i], map[string]string{"SKILL.md": "---\nname: Unique\n---\n"})
	}
	skills, supported, err := listRuntimeLocalSkills("devin")
	if err != nil || !supported || len(skills) != len(roots)+1 {
		t.Fatalf("list = %+v supported=%v err=%v", skills, supported, err)
	}
	for _, key := range append([]string{"duplicate"}, keys...) {
		bundle, supported, err := loadRuntimeLocalSkillBundle("devin", key)
		if err != nil || !supported {
			t.Fatalf("import %s: supported=%v err=%v", key, supported, err)
		}
		if key == "duplicate" && (bundle.Name != names[0] || bundle.SourcePath != relativizeHomePath(filepath.Join(roots[0], key))) {
			t.Fatalf("import did not match provider-first listing: %+v", bundle)
		}
	}
}
