package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDroidRuntimeBriefAndSkills(t *testing.T) {
	dir := t.TempDir()
	task := TaskContextForEnv{IssueID: "droid-issue", AgentSkills: []SkillContextForEnv{{Name: "Review", Content: "Review carefully."}}}
	if _, err := writeContextFiles(dir, "droid", task, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := InjectRuntimeConfig(dir, "droid", task); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"AGENTS.md": "Multica Agent Runtime", ".factory/skills/review/SKILL.md": "Review carefully."} {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(path)))
		if err != nil || !strings.Contains(string(data), want) {
			t.Fatalf("%s = %q, %v", path, data, err)
		}
	}
}
