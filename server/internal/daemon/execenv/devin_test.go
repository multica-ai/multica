package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevinPrepareDeliversAndCleansBriefAndSkills(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	briefPath := filepath.Join(workDir, "AGENTS.md")
	const original = "# Repository instructions\nPreserve user files.\n"
	if err := os.WriteFile(briefPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "devin-workspace", TaskID: "devin-task",
		Provider: "devin", LocalWorkDir: workDir,
		Task: TaskContextForEnv{
			IssueID:     "11111111-2222-3333-4444-555555555555",
			AgentSkills: []SkillContextForEnv{{Name: "review-helper", Content: "Review carefully."}},
		},
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InjectRuntimeConfig(env.WorkDir, "devin", TaskContextForEnv{IssueID: "11111111-2222-3333-4444-555555555555"}); err != nil {
		t.Fatal(err)
	}
	brief, err := os.ReadFile(briefPath)
	if err != nil || !strings.Contains(string(brief), original) || !strings.Contains(string(brief), "multica issue comment add") {
		t.Fatalf("missing brief or repository rules: %q err=%v", brief, err)
	}
	skillPath := filepath.Join(workDir, ".devin", "skills", "review-helper", "SKILL.md")
	skill, err := os.ReadFile(skillPath)
	if err != nil || !strings.Contains(string(skill), "Review carefully.") {
		t.Fatalf("native skill missing: %q err=%v", skill, err)
	}
	if err := CleanupSidecars(env.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := CleanupRuntimeConfig(workDir, "devin"); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(briefPath)
	if err != nil || string(restored) != original {
		t.Fatalf("user instructions not restored: %q err=%v", restored, err)
	}
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Fatalf("managed skill survived cleanup: %v", err)
	}
}
