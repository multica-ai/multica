package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTomcatWorktreeDeliversOnlyTaskChanges(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "tracked.txt"), "user edit\n")
	head := gitRun(t, repo, "rev-parse", "HEAD")
	task := TaskContextForEnv{IssueID: "issue-tomcat", AgentName: "Tomcat", AgentSkills: []SkillContextForEnv{{Name: "task-workflow", Content: "Report the task result."}}}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "ws-tomcat", TaskID: "11112222-3333-4444-5555-666677778888",
		AgentName: "Tomcat", Provider: "tomcat", LocalWorktree: &LocalWorktreeParams{LocalPath: repo}, Task: task,
	}, worktreeTestLogger())
	if err != nil {
		t.Fatal(err)
	}
	if env.LocalWorktree == nil || env.WorkDir != env.LocalWorktree.Path {
		t.Fatal("missing task worktree")
	}
	if _, err := InjectRuntimeConfig(env.WorkDir, "tomcat", task); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(env.WorkDir, "result.txt"), "task result\n")
	if err := CleanupRuntimeConfig(env.WorkDir, "tomcat"); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSidecars(env.RootDir); err != nil {
		t.Fatal(err)
	}
	outcome := finalizeOK(t, env.LocalWorktree)
	if outcome.Branch == "" {
		t.Fatal("missing delivery branch")
	}
	files := gitRun(t, repo, "diff", "--name-only", head, outcome.Branch)
	if !strings.Contains(files, "result.txt") {
		t.Fatalf("missing result: %s", files)
	}
	for _, sidecar := range []string{"AGENTS.md", ".agents", ".agent_context", ".multica"} {
		if strings.Contains(files, sidecar) {
			t.Fatalf("sidecar leaked: %s", files)
		}
	}
	if _, err := os.Stat(env.WorkDir); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
	if got := gitRun(t, repo, "status", "--porcelain"); got != "M tracked.txt" {
		t.Fatalf("user checkout changed: %s", got)
	}
}

func TestTomcatPrepareAndCleanupPreserveRepositoryFiles(t *testing.T) {
	work := t.TempDir()
	instructions := filepath.Join(work, "AGENTS.md")
	original := "# User instructions\nPreserve this file exactly."
	if err := os.WriteFile(instructions, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	env, err := Prepare(PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-tomcat", TaskID: "task-tomcat", Provider: "tomcat", LocalWorkDir: work, Task: TaskContextForEnv{IssueID: "11111111-2222-3333-4444-555555555555", AgentSkills: []SkillContextForEnv{{Name: "task-workflow", Content: "Read the task and report its result."}}}}, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := InjectRuntimeConfig(env.WorkDir, "tomcat", TaskContextForEnv{IssueID: "11111111-2222-3333-4444-555555555555"}); err != nil {
		t.Fatal(err)
	}
	brief, err := os.ReadFile(instructions)
	if err != nil || !strings.Contains(string(brief), original) || !strings.Contains(string(brief), "multica issue comment add") {
		t.Fatalf("brief=%q err=%v", brief, err)
	}
	skillPath := filepath.Join(work, ".agents", "skills", "task-workflow", "SKILL.md")
	skill, err := os.ReadFile(skillPath)
	if err != nil || !strings.Contains(string(skill), "Read the task and report its result.") {
		t.Fatalf("skill=%q err=%v", skill, err)
	}
	if err := CleanupSidecars(env.RootDir); err != nil {
		t.Fatal(err)
	}
	if err := CleanupRuntimeConfig(work, "tomcat"); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(instructions)
	if err != nil || string(restored) != original {
		t.Fatalf("restored=%q err=%v", restored, err)
	}
	if _, err := os.Stat(skillPath); !os.IsNotExist(err) {
		t.Fatalf("task skill survived cleanup: %v", err)
	}
}
