package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMusePrepareDeliversBriefAndSkills(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	briefPath := filepath.Join(workDir, "AGENTS.md")
	const repoInstructions = "# Repository rules\nKeep user files.\n"
	if err := os.WriteFile(briefPath, []byte(repoInstructions), 0o644); err != nil {
		t.Fatal(err)
	}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "muse-workspace", TaskID: "muse-task",
		Provider: "muse", LocalWorkDir: workDir,
		Task: TaskContextForEnv{
			IssueID:     "11111111-2222-3333-4444-555555555555",
			AgentSkills: []SkillContextForEnv{{Name: "muse-workflow", Content: "Follow the Muse workflow."}},
		},
	}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InjectRuntimeConfig(env.WorkDir, "muse", TaskContextForEnv{
		IssueID: "11111111-2222-3333-4444-555555555555",
	}); err != nil {
		t.Fatal(err)
	}
	brief, err := os.ReadFile(briefPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{repoInstructions, "multica issue comment add", "## Output"} {
		if !strings.Contains(string(brief), want) {
			t.Errorf("runtime brief is missing %q", want)
		}
	}
	skillPath := filepath.Join(workDir, ".agents", "skills", "muse-workflow", "SKILL.md")
	skill, err := os.ReadFile(skillPath)
	if err != nil || !strings.Contains(string(skill), "Follow the Muse workflow.") {
		t.Errorf("native skill missing: content=%q err=%v", skill, err)
	}
	if err := CleanupSidecars(env.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := CleanupRuntimeConfig(workDir, "muse"); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(briefPath)
	if err != nil || string(restored) != repoInstructions {
		t.Errorf("repository instructions changed after cleanup: %q, %v", restored, err)
	}
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Errorf("managed skill survived cleanup: %v", err)
	}
}
