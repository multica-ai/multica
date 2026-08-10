package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/codexcontext"
	"github.com/multica-ai/multica/server/pkg/skillbundle"
)

func TestPrepareOperationalCodexContextUsesOnlyAssignedSkills(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	t.Setenv("MULTICA_CODEX_MULTI_AGENT", "true")
	t.Setenv("MULTICA_CODEX_MEMORY", "true")
	mustWriteOperationalTestFile(t, filepath.Join(sharedHome, "auth.json"), "auth-sentinel")
	mustWriteOperationalTestFile(t, filepath.Join(sharedHome, "config.toml"), "shared-config-sentinel = true\n")
	mustWriteOperationalTestFile(t, filepath.Join(sharedHome, "instructions.md"), "ambient-instructions-sentinel")
	mustWriteOperationalTestFile(t, filepath.Join(sharedHome, "skills", "ambient-skill", "SKILL.md"), "ambient-skill-sentinel")

	ctx := buildOperationalTestContext(t, "assigned-skill-v1")
	params := PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "workspace-1",
		TaskID:         "task-12345678",
		Provider:       "codex",
		CodexContext:   &ctx,
		Task: TaskContextForEnv{
			AgentID: "agent-1",
			IssueID: "issue-1",
		},
	}

	env, err := Prepare(params, testLogger())
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	assertOperationalCodexHome(t, env)
	if _, err := os.Stat(filepath.Join(env.WorkDir, ".agent_context")); !os.IsNotExist(err) {
		t.Fatalf("operational workdir contains .agent_context: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.WorkDir, TaskContextMarkerRelPath)); err != nil {
		t.Fatalf("task context marker missing: %v", err)
	}

	config, err := os.ReadFile(filepath.Join(env.CodexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read operational config: %v", err)
	}
	for _, excluded := range []string{"shared-config-sentinel", "ambient-instructions-sentinel", "ambient-skill-sentinel"} {
		if strings.Contains(string(config), excluded) {
			t.Fatalf("operational config contains ambient sentinel %q", excluded)
		}
	}
	if !strings.Contains(string(config), "project_doc_max_bytes = 0") {
		t.Fatalf("project instructions are not disabled:\n%s", config)
	}
	if !strings.Contains(string(config), "[skills.bundled]") || !strings.Contains(string(config), "enabled = false") {
		t.Fatalf("bundled skills are not disabled:\n%s", config)
	}
	for _, required := range []string{
		"features.multi_agent = false",
		"features.memories = false",
		"memories.generate_memories = false",
		"memories.use_memories = false",
	} {
		if !strings.Contains(string(config), required) {
			t.Fatalf("operational config does not force %q:\n%s", required, config)
		}
	}
}

func TestReuseOperationalCodexContextReplacesStaleManagedState(t *testing.T) {
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)
	mustWriteOperationalTestFile(t, filepath.Join(sharedHome, "auth.json"), "auth-sentinel")

	ctx := buildOperationalTestContext(t, "assigned-skill-v1")
	params := PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "workspace-1",
		TaskID:         "task-12345678",
		Provider:       "codex",
		CodexContext:   &ctx,
		Task:           TaskContextForEnv{AgentID: "agent-1", IssueID: "issue-1"},
	}
	env, err := Prepare(params, testLogger())
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}

	mustWriteOperationalTestFile(t, filepath.Join(env.CodexHome, "skills", "stale-skill", "SKILL.md"), "stale-skill-sentinel")
	mustWriteOperationalTestFile(t, filepath.Join(env.CodexHome, "sessions", "stale-session.jsonl"), "stale-session-sentinel")
	mustWriteOperationalTestFile(t, filepath.Join(env.CodexHome, "stale-runtime-brief.md"), "stale-brief-sentinel")

	ctx = buildOperationalTestContext(t, "assigned-skill-v2")
	reused := Reuse(ReuseParams{
		WorkspacesRoot: params.WorkspacesRoot,
		WorkDir:        env.WorkDir,
		Provider:       "codex",
		CodexContext:   &ctx,
		Task:           params.Task,
	}, testLogger())
	if reused == nil {
		t.Fatal("Reuse() = nil")
	}

	assertOperationalCodexHome(t, reused)
	for _, stale := range []string{
		filepath.Join(reused.CodexHome, "skills", "stale-skill"),
		filepath.Join(reused.CodexHome, "sessions", "stale-session.jsonl"),
		filepath.Join(reused.CodexHome, "stale-runtime-brief.md"),
	} {
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Fatalf("stale operational artifact remains at %s: %v", stale, err)
		}
	}
	body, err := os.ReadFile(filepath.Join(reused.CodexHome, "skills", "runner-health", "SKILL.md"))
	if err != nil {
		t.Fatalf("read refreshed skill: %v", err)
	}
	if !strings.Contains(string(body), "assigned-skill-v2") {
		t.Fatalf("assigned skill was not refreshed: %s", body)
	}
}

func buildOperationalTestContext(t *testing.T, skillContent string) codexcontext.OperationalContext {
	t.Helper()
	ctx, err := codexcontext.BuildOperationalContext(codexcontext.BuildInput{
		AgentInstructions: "Operate as a bounded scout.",
		TaskPrompt:        "Inspect issue-1 and return the result.",
		AssignedSkills: []skillbundle.Skill{
			{
				Name:        "Runner Health",
				Description: "Inspect runner health.",
				Content:     skillContent,
				Files: []skillbundle.File{
					{Path: "references/contract.md", Content: "supporting-file-sentinel"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildOperationalContext() error = %v", err)
	}
	return ctx
}

func assertOperationalCodexHome(t *testing.T, env *Environment) {
	t.Helper()
	if env.CodexHome == "" {
		t.Fatal("CodexHome is empty")
	}
	if _, err := os.Stat(filepath.Join(env.CodexHome, "auth.json")); err != nil {
		t.Fatalf("auth link missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.CodexHome, "skills", "runner-health", "SKILL.md")); err != nil {
		t.Fatalf("assigned skill missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.CodexHome, "skills", "runner-health", "references", "contract.md")); err != nil {
		t.Fatalf("assigned skill supporting file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.CodexHome, "skills", "ambient-skill")); !os.IsNotExist(err) {
		t.Fatalf("ambient skill visible: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(env.CodexHome, "sessions"))
	if err != nil {
		t.Fatalf("read sessions dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("operational sessions are not fresh: %#v", entries)
	}
}

func mustWriteOperationalTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
