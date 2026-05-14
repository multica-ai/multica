package execenv

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.Default()
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestShortID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input, want string
	}{
		{"a1b2c3d4-e5f6-7890-abcd-ef1234567890", "a1b2c3d4"},
		{"abcdef12", "abcdef12"},
		{"ab", "ab"},
		{"a1b2c3d4e5f67890", "a1b2c3d4"},
	}
	for _, tt := range tests {
		if got := shortID(tt.input); got != tt.want {
			t.Errorf("shortID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPredictRootDir(t *testing.T) {
	t.Parallel()
	got := PredictRootDir("/root", "ws-uuid", "a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	want := filepath.Join("/root", "ws-uuid", "a1b2c3d4")
	if got != want {
		t.Errorf("PredictRootDir = %q, want %q", got, want)
	}
	if got := PredictRootDir("", "ws", "task"); got != "" {
		t.Errorf("expected empty when workspaces root missing, got %q", got)
	}
	if got := PredictRootDir("/r", "", "task"); got != "" {
		t.Errorf("expected empty when workspace ID missing, got %q", got)
	}
	if got := PredictRootDir("/r", "ws", ""); got != "" {
		t.Errorf("expected empty when task ID missing, got %q", got)
	}
}

func TestSanitizeName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input, want string
	}{
		{"Code Reviewer", "code-reviewer"},
		{"my_agent!@#v2", "my-agent-v2"},
		{"  spaces  ", "spaces"},
		{"UPPERCASE", "uppercase"},
		{"a-very-long-name-that-exceeds-thirty-characters-total", "a-very-long-name-that-exceeds"},
		{"", "agent"},
		{"---", "agent"},
		{"日本語テスト", "agent"},
	}
	for _, tt := range tests {
		if got := sanitizeName(tt.input); got != tt.want {
			t.Errorf("sanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRepoNameFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input, want string
	}{
		{"https://github.com/org/my-repo.git", "my-repo"},
		{"https://github.com/org/my-repo", "my-repo"},
		{"git@github.com:org/my-repo.git", "my-repo"},
		{"https://github.com/org/repo/", "repo"},
		{"my-repo", "my-repo"},
		{"", "repo"},
	}
	for _, tt := range tests {
		if got := repoNameFromURL(tt.input); got != tt.want {
			t.Errorf("repoNameFromURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPrepareDirectoryMode(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-test-001",
		TaskID:         "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		AgentName:      "Test Agent",
		Task: TaskContextForEnv{
			IssueID: "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			AgentSkills: []SkillContextForEnv{
				{Name: "Code Review", Content: "Be concise."},
			},
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	// Verify directory structure.
	for _, sub := range []string{"workdir", "output", "logs"} {
		path := filepath.Join(env.RootDir, sub)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Fatalf("expected %s to exist", path)
		}
	}

	// Verify context file contains issue ID and CLI hints.
	content, err := os.ReadFile(filepath.Join(env.WorkDir, ".agent_context", "issue_context.md"))
	if err != nil {
		t.Fatalf("failed to read issue_context.md: %v", err)
	}
	for _, want := range []string{"a1b2c3d4-e5f6-7890-abcd-ef1234567890", "Code Review"} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("issue_context.md missing %q", want)
		}
	}

	// Verify skill files.
	skillContent, err := os.ReadFile(filepath.Join(env.WorkDir, ".agent_context", "skills", "code-review", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read SKILL.md: %v", err)
	}
	if !strings.Contains(string(skillContent), "Be concise.") {
		t.Fatal("SKILL.md missing content")
	}
}

func TestPrepareWithProjectResources(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()

	taskCtx := TaskContextForEnv{
		IssueID:      "11111111-2222-3333-4444-555555555555",
		ProjectID:    "22222222-3333-4444-5555-666666666666",
		ProjectTitle: "Agent UX 2026",
		ProjectResources: []ProjectResourceForEnv{
			{
				ID:           "33333333-4444-5555-6666-777777777777",
				ResourceType: "github_repo",
				ResourceRef:  json.RawMessage(`{"url":"https://github.com/multica-ai/multica","default_branch_hint":"main"}`),
			},
		},
	}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-test-pr",
		TaskID:         "11111111-2222-3333-4444-555555555555",
		AgentName:      "Test Agent",
		Provider:       "claude",
		Task:           taskCtx,
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	// resources.json should exist and decode back to what we wrote.
	resourcesPath := filepath.Join(env.WorkDir, ".multica", "project", "resources.json")
	raw, err := os.ReadFile(resourcesPath)
	if err != nil {
		t.Fatalf("failed to read resources.json: %v", err)
	}
	var got struct {
		ProjectID    string `json:"project_id"`
		ProjectTitle string `json:"project_title"`
		Resources    []struct {
			ID           string          `json:"id"`
			ResourceType string          `json:"resource_type"`
			ResourceRef  json.RawMessage `json:"resource_ref"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("resources.json unmarshal: %v\n%s", err, string(raw))
	}
	if got.ProjectID != taskCtx.ProjectID {
		t.Errorf("resources.json project_id = %q, want %q", got.ProjectID, taskCtx.ProjectID)
	}
	if got.ProjectTitle != taskCtx.ProjectTitle {
		t.Errorf("resources.json project_title = %q, want %q", got.ProjectTitle, taskCtx.ProjectTitle)
	}
	if len(got.Resources) != 1 || got.Resources[0].ResourceType != "github_repo" {
		t.Fatalf("resources.json resources mismatch: %+v", got.Resources)
	}

	// CLAUDE.md should mention the project context block.
	if _, err := InjectRuntimeConfig(env.WorkDir, "claude", taskCtx); err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(env.WorkDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	s := string(content)
	for _, want := range []string{
		"## Project Context",
		"Agent UX 2026",
		"GitHub repo",
		"https://github.com/multica-ai/multica",
		"default branch: `main`",
		".multica/project/resources.json",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("CLAUDE.md missing %q", want)
		}
	}
}

// When the issue's project has its own github_repo resources, those should be
// the only repos rendered in the meta-skill — workspace-level repos must not
// leak into the agent prompt to avoid confusing it about which repo to use.
//
// The handler-side override is exercised in handler tests; this test confirms
// the rendering side: given a TaskContextForEnv where Repos was already
// narrowed by the server to project repos only, the meta skill renders just
// those.
func TestProjectReposReplaceWorkspaceReposInMetaSkill(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ctx := TaskContextForEnv{
		IssueID:      "11111111-2222-3333-4444-555555555555",
		ProjectID:    "22222222-3333-4444-5555-666666666666",
		ProjectTitle: "Project A",
		Repos: []RepoContextForEnv{
			{URL: "https://github.com/org/project-repo"},
		},
		ProjectResources: []ProjectResourceForEnv{
			{
				ID:           "33333333-4444-5555-6666-777777777777",
				ResourceType: "github_repo",
				ResourceRef:  []byte(`{"url":"https://github.com/org/project-repo"}`),
			},
		},
	}
	if _, err := InjectRuntimeConfig(dir, "claude", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	s := string(content)
	if !strings.Contains(s, "https://github.com/org/project-repo") {
		t.Errorf("CLAUDE.md missing project repo URL")
	}
	if strings.Contains(s, "https://github.com/org/workspace-repo") {
		t.Errorf("CLAUDE.md should not contain workspace repo when project has its own")
	}
}

func TestWriteProjectResourcesSkippedWhenNone(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := writeProjectResources(dir, TaskContextForEnv{}); err != nil {
		t.Fatalf("writeProjectResources: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".multica", "project", "resources.json")); !os.IsNotExist(err) {
		t.Errorf("expected no resources.json to be written when project context is empty")
	}
}

func TestPrepareWithRepoContext(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()

	taskCtx := TaskContextForEnv{
		IssueID: "b2c3d4e5-f6a7-8901-bcde-f12345678901",
		Repos: []RepoContextForEnv{
			{URL: "https://github.com/org/backend"},
			{URL: "https://github.com/org/frontend"},
		},
	}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-test-002",
		TaskID:         "b2c3d4e5-f6a7-8901-bcde-f12345678901",
		AgentName:      "Code Reviewer",
		Provider:       "claude",
		Task:           taskCtx,
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	// Inject runtime config (done separately in daemon, replicate here).
	if _, err := InjectRuntimeConfig(env.WorkDir, "claude", taskCtx); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}

	// Workdir should be empty (no pre-created repo dirs).
	entries, err := os.ReadDir(env.WorkDir)
	if err != nil {
		t.Fatalf("failed to read workdir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if name != ".agent_context" && name != "CLAUDE.md" && name != ".claude" {
			t.Errorf("unexpected entry in workdir: %s", name)
		}
	}

	// CLAUDE.md should contain repo info.
	content, err := os.ReadFile(filepath.Join(env.WorkDir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}
	s := string(content)
	for _, want := range []string{
		"multica repo checkout",
		"https://github.com/org/backend",
		"https://github.com/org/frontend",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("CLAUDE.md missing %q", want)
		}
	}
}

func TestWriteContextFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID: "test-issue-id-1234",
		AgentSkills: []SkillContextForEnv{
			{
				Name:    "Go Conventions",
				Content: "Follow Go conventions.",
				Files: []SkillFileContextForEnv{
					{Path: "templates/example.go", Content: "package main"},
				},
			},
		},
	}

	if err := writeContextFiles(dir, "", ctx); err != nil {
		t.Fatalf("writeContextFiles failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".agent_context", "issue_context.md"))
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	s := string(content)
	for _, want := range []string{
		"test-issue-id-1234",
		"## Agent Skills",
		"Go Conventions",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("content missing %q", want)
		}
	}

	// Issue details should NOT be in the context file (agent fetches via CLI).
	for _, absent := range []string{"## Description", "## Workspace Context"} {
		if strings.Contains(s, absent) {
			t.Errorf("content should NOT contain %q — agent fetches details via CLI", absent)
		}
	}

	// Verify skill directory and files.
	skillMd, err := os.ReadFile(filepath.Join(dir, ".agent_context", "skills", "go-conventions", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read SKILL.md: %v", err)
	}
	if !strings.Contains(string(skillMd), "Follow Go conventions.") {
		t.Error("SKILL.md missing content")
	}

	supportFile, err := os.ReadFile(filepath.Join(dir, ".agent_context", "skills", "go-conventions", "templates", "example.go"))
	if err != nil {
		t.Fatalf("failed to read supporting file: %v", err)
	}
	if string(supportFile) != "package main" {
		t.Errorf("supporting file content = %q, want %q", string(supportFile), "package main")
	}
}

func TestWriteContextFilesOmitsSkillsWhenEmpty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID: "minimal-issue-id",
	}

	if err := writeContextFiles(dir, "", ctx); err != nil {
		t.Fatalf("writeContextFiles failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".agent_context", "issue_context.md"))
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "minimal-issue-id") {
		t.Error("expected issue ID to be present")
	}
	if strings.Contains(s, "## Agent Skills") {
		t.Error("expected skills section to be omitted when no skills")
	}
}

func TestWriteContextFilesAutopilotRunOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		AutopilotRunID:       "run-1",
		AutopilotID:          "autopilot-1",
		AutopilotTitle:       "Daily dependency check",
		AutopilotDescription: "Check dependencies and report outdated packages.",
		AutopilotSource:      "manual",
	}

	if err := writeContextFiles(dir, "", ctx); err != nil {
		t.Fatalf("writeContextFiles failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, ".agent_context", "issue_context.md"))
	if err != nil {
		t.Fatalf("failed to read: %v", err)
	}

	s := string(content)
	for _, want := range []string{
		"# Autopilot Run",
		"run-1",
		"autopilot-1",
		"Check dependencies and report outdated packages.",
		"multica autopilot get autopilot-1 --output json",
		"no assigned issue",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("autopilot context missing %q\n---\n%s", want, s)
		}
	}
	if strings.Contains(s, "Run `multica issue get") {
		t.Errorf("autopilot context should not contain issue get workflow\n---\n%s", s)
	}
}

func TestWriteContextFilesClaudeNativeSkills(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID: "claude-skill-test",
		AgentSkills: []SkillContextForEnv{
			{
				Name:    "Go Conventions",
				Content: "Follow Go conventions.",
				Files: []SkillFileContextForEnv{
					{Path: "templates/example.go", Content: "package main"},
				},
			},
		},
	}

	if err := writeContextFiles(dir, "claude", ctx); err != nil {
		t.Fatalf("writeContextFiles failed: %v", err)
	}

	// Skills should be in .claude/skills/ (native discovery), NOT .agent_context/skills/.
	skillMd, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "go-conventions", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read .claude/skills/go-conventions/SKILL.md: %v", err)
	}
	if !strings.Contains(string(skillMd), "Follow Go conventions.") {
		t.Error("SKILL.md missing content")
	}

	// Supporting files should also be under .claude/skills/.
	supportFile, err := os.ReadFile(filepath.Join(dir, ".claude", "skills", "go-conventions", "templates", "example.go"))
	if err != nil {
		t.Fatalf("failed to read supporting file: %v", err)
	}
	if string(supportFile) != "package main" {
		t.Errorf("supporting file content = %q, want %q", string(supportFile), "package main")
	}

	// .agent_context/skills/ should NOT exist for Claude.
	if _, err := os.Stat(filepath.Join(dir, ".agent_context", "skills")); !os.IsNotExist(err) {
		t.Error("expected .agent_context/skills/ to NOT exist for Claude provider")
	}

	// issue_context.md should still be in .agent_context/.
	if _, err := os.Stat(filepath.Join(dir, ".agent_context", "issue_context.md")); os.IsNotExist(err) {
		t.Error("expected .agent_context/issue_context.md to exist")
	}
}

func TestCleanupPreservesLogs(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-test-003",
		TaskID:         "d4e5f6a7-b8c9-0123-defa-234567890123",
		AgentName:      "Preserve Test",
		Task:           TaskContextForEnv{IssueID: "preserve-test-id"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Write something to logs/.
	os.WriteFile(filepath.Join(env.RootDir, "logs", "test.log"), []byte("log data"), 0o644)

	// Cleanup with removeAll=false.
	if err := env.Cleanup(false); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// workdir should be gone.
	if _, err := os.Stat(env.WorkDir); !os.IsNotExist(err) {
		t.Fatal("expected workdir to be removed")
	}

	// logs should still exist.
	logFile := filepath.Join(env.RootDir, "logs", "test.log")
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		t.Fatal("expected logs/test.log to be preserved")
	}
}

func TestInjectRuntimeConfigClaude(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID: "test-issue-id",
		AgentSkills: []SkillContextForEnv{
			{Name: "Go Conventions", Content: "Follow Go conventions.", Files: []SkillFileContextForEnv{
				{Path: "example.go", Content: "package main"},
			}},
			{Name: "PR Review", Content: "Review PRs carefully."},
		},
	}

	if _, err := InjectRuntimeConfig(dir, "claude", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}

	s := string(content)
	for _, want := range []string{
		"Multica Agent Runtime",
		"multica issue get",
		"multica issue comment list",
		"Go Conventions",
		"PR Review",
		"discovered automatically",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("CLAUDE.md missing %q", want)
		}
	}
}

// Regression test for #2347: the runtime config injected into agent harnesses
// must advertise both autopilot execution modes on create AND update, so an
// agent acting as a CLI user is not confined to create_issue.
func TestInjectRuntimeConfigAutopilotAdvertisesBothModes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if _, err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{IssueID: "issue-1"}); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}

	s := string(content)
	for _, want := range []string{
		"multica autopilot create --title \"...\" --agent <name> --mode create_issue|run_only",
		"multica autopilot update <id>",
		"[--mode create_issue|run_only]",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("CLAUDE.md missing %q", want)
		}
	}
}

func TestInjectRuntimeConfigGemini(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID:     "test-issue-id",
		AgentSkills: []SkillContextForEnv{{Name: "Writing", Content: "Write clearly."}},
	}

	if _, err := InjectRuntimeConfig(dir, "gemini", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "GEMINI.md"))
	if err != nil {
		t.Fatalf("failed to read GEMINI.md: %v", err)
	}

	s := string(content)
	for _, want := range []string{
		"Multica Agent Runtime",
		"multica issue get",
		"Writing",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("GEMINI.md missing %q", want)
		}
	}

	// Should not write CLAUDE.md or AGENTS.md for gemini provider.
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("gemini provider should not create CLAUDE.md")
	}
	if _, err := os.Stat(filepath.Join(dir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("gemini provider should not create AGENTS.md")
	}
}

func TestInjectRuntimeConfigCodex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID:     "test-issue-id",
		AgentSkills: []SkillContextForEnv{{Name: "Coding", Content: "Write good code."}},
	}

	if _, err := InjectRuntimeConfig(dir, "codex", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.md: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "Multica Agent Runtime") {
		t.Error("AGENTS.md missing meta skill header")
	}
	if !strings.Contains(s, "Coding") {
		t.Error("AGENTS.md missing skill name")
	}
}

func TestInjectRuntimeConfigNoSkills(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{IssueID: "test-issue-id"}

	if _, err := InjectRuntimeConfig(dir, "claude", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "multica issue get") {
		t.Error("should reference multica CLI even without skills")
	}
	if strings.Contains(s, "## Skills") {
		t.Error("should not have Skills section when there are no skills")
	}
}

func TestWriteContextFilesCopilotNativeSkills(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID: "copilot-skill-test",
		AgentSkills: []SkillContextForEnv{
			{
				Name:    "Go Conventions",
				Content: "Follow Go conventions.",
				Files: []SkillFileContextForEnv{
					{Path: "templates/example.go", Content: "package main"},
				},
			},
		},
	}

	if err := writeContextFiles(dir, "copilot", ctx); err != nil {
		t.Fatalf("writeContextFiles failed: %v", err)
	}

	// Copilot CLI natively discovers project-level skills from .github/skills/.
	skillMd, err := os.ReadFile(filepath.Join(dir, ".github", "skills", "go-conventions", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read .github/skills/go-conventions/SKILL.md: %v", err)
	}
	if !strings.Contains(string(skillMd), "Follow Go conventions.") {
		t.Error("SKILL.md missing content")
	}

	// Supporting files should also be under .github/skills/.
	supportFile, err := os.ReadFile(filepath.Join(dir, ".github", "skills", "go-conventions", "templates", "example.go"))
	if err != nil {
		t.Fatalf("failed to read supporting file: %v", err)
	}
	if string(supportFile) != "package main" {
		t.Errorf("supporting file content = %q, want %q", string(supportFile), "package main")
	}

	// .agent_context/skills/ should NOT exist for Copilot.
	if _, err := os.Stat(filepath.Join(dir, ".agent_context", "skills")); !os.IsNotExist(err) {
		t.Error("expected .agent_context/skills/ to NOT exist for Copilot provider")
	}

	// issue_context.md should still be in .agent_context/.
	if _, err := os.Stat(filepath.Join(dir, ".agent_context", "issue_context.md")); os.IsNotExist(err) {
		t.Error("expected .agent_context/issue_context.md to exist")
	}
}

func TestWriteContextFilesOpencodeNativeSkills(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID: "opencode-skill-test",
		AgentSkills: []SkillContextForEnv{
			{
				Name:    "Go Conventions",
				Content: "Follow Go conventions.",
				Files: []SkillFileContextForEnv{
					{Path: "templates/example.go", Content: "package main"},
				},
			},
		},
	}

	if err := writeContextFiles(dir, "opencode", ctx); err != nil {
		t.Fatalf("writeContextFiles failed: %v", err)
	}

	// Skills should be in .opencode/skills/ (native discovery).
	skillMd, err := os.ReadFile(filepath.Join(dir, ".opencode", "skills", "go-conventions", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read .opencode/skills/go-conventions/SKILL.md: %v", err)
	}
	if !strings.Contains(string(skillMd), "Follow Go conventions.") {
		t.Error("SKILL.md missing content")
	}

	// Supporting files should also be under .opencode/skills/.
	supportFile, err := os.ReadFile(filepath.Join(dir, ".opencode", "skills", "go-conventions", "templates", "example.go"))
	if err != nil {
		t.Fatalf("failed to read supporting file: %v", err)
	}
	if string(supportFile) != "package main" {
		t.Errorf("supporting file content = %q, want %q", string(supportFile), "package main")
	}

	// .agent_context/skills/ should NOT exist for OpenCode.
	if _, err := os.Stat(filepath.Join(dir, ".agent_context", "skills")); !os.IsNotExist(err) {
		t.Error("expected .agent_context/skills/ to NOT exist for OpenCode provider")
	}

	// issue_context.md should still be in .agent_context/.
	if _, err := os.Stat(filepath.Join(dir, ".agent_context", "issue_context.md")); os.IsNotExist(err) {
		t.Error("expected .agent_context/issue_context.md to exist")
	}
}

// OpenClaw scans its own workspaceDir (resolved from the openclaw config,
// not the task workdir) and never reads {workDir}/.openclaw/skills/. Until
// per-task workspace integration lands, openclaw skills fall back to
// .agent_context/skills/ — the meta AGENTS.md content references that path
// explicitly. This test fails closed if someone re-adds a dead-drop case to
// resolveSkillsDir.
func TestWriteContextFilesOpenclawFallsBackToAgentContext(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID: "openclaw-skill-test",
		AgentSkills: []SkillContextForEnv{
			{
				Name:    "Go Conventions",
				Content: "Follow Go conventions.",
				Files: []SkillFileContextForEnv{
					{Path: "templates/example.go", Content: "package main"},
				},
			},
		},
	}

	if err := writeContextFiles(dir, "openclaw", ctx); err != nil {
		t.Fatalf("writeContextFiles failed: %v", err)
	}

	skillMd, err := os.ReadFile(filepath.Join(dir, ".agent_context", "skills", "go-conventions", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read .agent_context/skills/go-conventions/SKILL.md: %v", err)
	}
	if !strings.Contains(string(skillMd), "Follow Go conventions.") {
		t.Error("SKILL.md missing content")
	}

	supportFile, err := os.ReadFile(filepath.Join(dir, ".agent_context", "skills", "go-conventions", "templates", "example.go"))
	if err != nil {
		t.Fatalf("failed to read supporting file: %v", err)
	}
	if string(supportFile) != "package main" {
		t.Errorf("supporting file content = %q, want %q", string(supportFile), "package main")
	}

	if _, err := os.Stat(filepath.Join(dir, ".openclaw", "skills")); !os.IsNotExist(err) {
		t.Error(".openclaw/skills/ MUST NOT be written — openclaw never scans that path; writing there is a dead drop")
	}
}

func TestWriteContextFilesKiroNativeSkills(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID: "kiro-skill-test",
		AgentSkills: []SkillContextForEnv{
			{Name: "Go Conventions", Content: "Follow Go conventions."},
		},
	}

	if err := writeContextFiles(dir, "kiro", ctx); err != nil {
		t.Fatalf("writeContextFiles failed: %v", err)
	}

	skillMd, err := os.ReadFile(filepath.Join(dir, ".kiro", "skills", "go-conventions", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read .kiro/skills/go-conventions/SKILL.md: %v", err)
	}
	if !strings.Contains(string(skillMd), "Follow Go conventions.") {
		t.Error("SKILL.md missing content")
	}
	if _, err := os.Stat(filepath.Join(dir, ".agent_context", "skills")); !os.IsNotExist(err) {
		t.Error("expected .agent_context/skills/ to NOT exist for Kiro provider")
	}
}

func TestInjectRuntimeConfigOpencode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID:     "test-issue-id",
		AgentSkills: []SkillContextForEnv{{Name: "Coding", Content: "Write good code."}},
	}

	if _, err := InjectRuntimeConfig(dir, "opencode", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}

	// OpenCode uses AGENTS.md (same as codex).
	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.md: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "Multica Agent Runtime") {
		t.Error("AGENTS.md missing meta skill header")
	}
	if !strings.Contains(s, "Coding") {
		t.Error("AGENTS.md missing skill name")
	}
	if !strings.Contains(s, "discovered automatically") {
		t.Error("AGENTS.md missing native skill discovery hint")
	}

	// CLAUDE.md should NOT exist.
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("expected CLAUDE.md to NOT exist for OpenCode provider")
	}
}

func TestInjectRuntimeConfigKiro(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID:     "test-issue-id",
		AgentSkills: []SkillContextForEnv{{Name: "Coding", Content: "Write good code."}},
	}

	if _, err := InjectRuntimeConfig(dir, "kiro", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.md: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "Multica Agent Runtime") {
		t.Error("AGENTS.md missing meta skill header")
	}
	if !strings.Contains(s, "Coding") {
		t.Error("AGENTS.md missing skill name")
	}
	if !strings.Contains(s, "discovered automatically") {
		t.Error("AGENTS.md missing native skill discovery hint")
	}
}

func TestPrepareWithRepoContextOpencode(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()

	taskCtx := TaskContextForEnv{
		IssueID: "c3d4e5f6-a7b8-9012-cdef-123456789012",
		Repos: []RepoContextForEnv{
			{URL: "https://github.com/org/backend"},
		},
	}
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-test-oc",
		TaskID:         "c3d4e5f6-a7b8-9012-cdef-123456789012",
		AgentName:      "OpenCode Agent",
		Provider:       "opencode",
		Task:           taskCtx,
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if _, err := InjectRuntimeConfig(env.WorkDir, "opencode", taskCtx); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}

	// Workdir should only contain expected entries.
	entries, err := os.ReadDir(env.WorkDir)
	if err != nil {
		t.Fatalf("failed to read workdir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if name != ".agent_context" && name != "AGENTS.md" {
			t.Errorf("unexpected entry in workdir: %s", name)
		}
	}

	// AGENTS.md should contain repo info.
	content, err := os.ReadFile(filepath.Join(env.WorkDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.md: %v", err)
	}
	s := string(content)
	for _, want := range []string{
		"multica repo checkout",
		"https://github.com/org/backend",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("AGENTS.md missing %q", want)
		}
	}
}

// TestInjectRuntimeConfigRequiresExplicitCommentPost ensures the injected
// workflow makes "post a comment with results" an explicit, unmissable step in
// both the assignment- and comment-triggered branches, plus hard-warns in the
// Output section that terminal/log text is not user-visible. Agents were
// silently finishing tasks without ever posting their result to the issue; see
// MUL-1124. Covering this in a test prevents the guidance from decaying back
// into a nested clause again.
func TestInjectRuntimeConfigRequiresExplicitCommentPost(t *testing.T) {
	t.Parallel()

	assignmentCtx := TaskContextForEnv{IssueID: "issue-1"}
	commentCtx := TaskContextForEnv{IssueID: "issue-1", TriggerCommentID: "comment-1"}

	for _, tc := range []struct {
		name string
		ctx  TaskContextForEnv
	}{
		{"assignment-triggered", assignmentCtx},
		{"comment-triggered", commentCtx},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if _, err := InjectRuntimeConfig(dir, "claude", tc.ctx); err != nil {
				t.Fatalf("InjectRuntimeConfig failed: %v", err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
			if err != nil {
				t.Fatalf("read CLAUDE.md: %v", err)
			}
			s := string(data)

			// The workflow must contain an explicit `multica issue comment add`
			// invocation for this issue — not just a prose mention of posting.
			mustContain := []string{
				"multica issue comment add issue-1",
				"mandatory",
			}
			for _, want := range mustContain {
				if !strings.Contains(s, want) {
					t.Errorf("%s: CLAUDE.md missing %q\n---\n%s", tc.name, want, s)
				}
			}

			// The Output section must carry a hard warning that terminal/log
			// output is not user-visible. This is the second line of defense
			// in case the agent skips past the workflow steps.
			for _, want := range []string{
				"Final results MUST be delivered via `multica issue comment add`",
				"does NOT see your terminal output",
			} {
				if !strings.Contains(s, want) {
					t.Errorf("%s: Output warning missing %q", tc.name, want)
				}
			}
		})
	}
}

// TestInjectRuntimeConfigAvailableCommandsIsNeutral pins that the global
// Available Commands section lists the three input modes neutrally for
// every non-Codex provider on every host OS, with no "MUST pipe via stdin"
// mandate.
//
// Background: #1795 / #1851 introduced "MUST pipe via stdin" /
// `--description-stdin` directives in the global section to fix Codex's
// habit of emitting literal `\n` inside `--content "..."` (MUL-1467).
// That mandate landed in the all-provider section and ended up steering
// every provider at stdin — which then broke non-ASCII bytes on Windows
// shells (#2198 / #2236 / #2376). This rollback keeps the strong
// Codex-specific mandate in the Codex-Specific section (pinned by
// TestInjectRuntimeConfigCodexLinuxEmphasizesStdin) and leaves the global
// section neutral.
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestInjectRuntimeConfigAvailableCommandsIsNeutral(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })

	for _, host := range []string{"linux", "darwin", "windows"} {
		for _, provider := range []string{"claude", "opencode", "openclaw", "hermes", "kimi", "kiro", "cursor", "gemini"} {
			t.Run(provider+"/"+host, func(t *testing.T) {
				runtimeGOOS = host
				dir := t.TempDir()
				if _, err := InjectRuntimeConfig(dir, provider, TaskContextForEnv{IssueID: "issue-1"}); err != nil {
					t.Fatalf("InjectRuntimeConfig failed: %v", err)
				}

				configFile := "CLAUDE.md"
				if provider != "claude" {
					configFile = "AGENTS.md"
				}
				if provider == "gemini" {
					configFile = "GEMINI.md"
				}
				data, err := os.ReadFile(filepath.Join(dir, configFile))
				if err != nil {
					t.Fatalf("read %s: %v", configFile, err)
				}
				s := string(data)

				// Available Commands lists all three input modes as fact.
				for _, want := range []string{
					"`--content \"...\"`",
					"`--content-stdin`",
					"`--content-file <path>`",
					"`--description-stdin`",
					"`--description-file <path>`",
				} {
					if !strings.Contains(s, want) {
						t.Errorf("%s missing flag mention %q\n---\n%s", configFile, want, s)
					}
				}

				// "MUST pipe via stdin" must NOT appear in any non-Codex
				// provider's runtime config: it was the over-spread of
				// the Codex-specific fix.
				for _, banned := range []string{
					"MUST pipe via stdin",
					"Agent-authored comments should always pipe content via stdin",
					"use `--description-stdin` and pipe a HEREDOC",
				} {
					if strings.Contains(s, banned) {
						t.Errorf("%s carries over-spread Codex mandate %q for non-Codex provider %s\n---\n%s", configFile, banned, provider, s)
					}
				}
			})
		}
	}
}

// TestInjectRuntimeConfigCodexLinuxEmphasizesStdin pins the
// Codex-Specific Comment Formatting section's "MUST stdin" mandate on
// non-Windows hosts. This is the MUL-1467 / #1795 / #1851 fix scoped
// back to where it belongs.
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestInjectRuntimeConfigCodexLinuxEmphasizesStdin(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "linux"

	dir := t.TempDir()
	if _, err := InjectRuntimeConfig(dir, "codex", TaskContextForEnv{
		IssueID:          "issue-1",
		TriggerCommentID: "comment-1",
	}); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	s := string(data)

	for _, want := range []string{
		"Codex-Specific Comment Formatting",
		"always use `--content-stdin` with a HEREDOC",
		"even for short single-line replies",
		"Never use inline `--content` for agent-authored comments",
		"Keep the same `--parent` value",
		"do not rely on `\\n` escapes",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("AGENTS.md missing Codex multiline guidance %q\n---\n%s", want, s)
		}
	}
}

// TestInjectRuntimeConfigCodexWindowsUsesContentFile pins that on Windows
// the Codex-Specific section directs the agent at `--content-file` instead
// of `--content-stdin`. PowerShell 5.1 / cmd.exe re-encode piped HEREDOC
// bytes through the active console codepage and silently drop non-ASCII
// as `?` before reaching `multica.exe` (#2198 / #2236 / #2376).
//
// Not parallel: mutates the package-level runtimeGOOS.
func TestInjectRuntimeConfigCodexWindowsUsesContentFile(t *testing.T) {
	saved := runtimeGOOS
	t.Cleanup(func() { runtimeGOOS = saved })
	runtimeGOOS = "windows"

	dir := t.TempDir()
	if _, err := InjectRuntimeConfig(dir, "codex", TaskContextForEnv{IssueID: "issue-1"}); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	s := string(data)
	for _, want := range []string{
		"On Windows, **always write the comment body to a UTF-8 file",
		"$OutputEncoding",
		"--content-file",
		"silently dropping non-ASCII characters as `?`",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("AGENTS.md missing Codex/Windows file-first guidance %q\n---\n%s", want, s)
		}
	}
	for _, banned := range []string{
		"always use `--content-stdin` with a HEREDOC, even for short single-line replies",
	} {
		if strings.Contains(s, banned) {
			t.Errorf("AGENTS.md still carries Codex stdin mandate %q on Windows\n---\n%s", banned, s)
		}
	}
}

func TestInjectRuntimeConfigAutopilotRunOnlyNoIssueWorkflow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		AutopilotRunID:       "run-1",
		AutopilotID:          "autopilot-1",
		AutopilotTitle:       "Daily dependency check",
		AutopilotDescription: "Check dependencies and report outdated packages.",
		AutopilotSource:      "manual",
	}

	if _, err := InjectRuntimeConfig(dir, "codex", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	s := string(data)

	for _, want := range []string{
		"Autopilot in run-only mode",
		"Autopilot run ID: `run-1`",
		"Check dependencies and report outdated packages.",
		"multica autopilot get autopilot-1 --output json",
		"Your final assistant output is captured automatically as the autopilot run result",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("autopilot runtime config missing %q\n---\n%s", want, s)
		}
	}

	for _, absent := range []string{
		"Run `multica issue get",
		"Final results MUST be delivered via `multica issue comment add`",
	} {
		if strings.Contains(s, absent) {
			t.Errorf("autopilot runtime config should not contain %q\n---\n%s", absent, s)
		}
	}
}

func TestInjectRuntimeConfigUnknownProvider(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Unknown provider should be a no-op.
	if _, err := InjectRuntimeConfig(dir, "unknown", TaskContextForEnv{}); err != nil {
		t.Fatalf("expected no error for unknown provider, got: %v", err)
	}

	// No files should be created.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected empty dir for unknown provider, got %d entries", len(entries))
	}
}

func TestInjectRuntimeConfigHermes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID:     "test-issue-id",
		AgentSkills: []SkillContextForEnv{{Name: "Coding", Content: "Write good code."}},
	}

	if _, err := InjectRuntimeConfig(dir, "hermes", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}

	// Hermes uses AGENTS.md.
	content, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("failed to read AGENTS.md: %v", err)
	}

	s := string(content)
	if !strings.Contains(s, "Multica Agent Runtime") {
		t.Error("AGENTS.md missing meta skill header")
	}
	if !strings.Contains(s, "Coding") {
		t.Error("AGENTS.md missing skill name")
	}
	// Hermes has no native skill discovery path wired up, so AGENTS.md must
	// point the agent at the .agent_context/skills/ fallback — NOT claim that
	// skills are "discovered automatically".
	if strings.Contains(s, "discovered automatically") {
		t.Error("AGENTS.md for Hermes should not claim native skill discovery")
	}
	if !strings.Contains(s, ".agent_context/skills/") {
		t.Error("AGENTS.md for Hermes should reference .agent_context/skills/ fallback path")
	}

	// CLAUDE.md should NOT exist.
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("expected CLAUDE.md to NOT exist for Hermes provider")
	}
}

func TestWriteContextFilesHermesFallbackSkills(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID: "hermes-skill-test",
		AgentSkills: []SkillContextForEnv{
			{Name: "Go Conventions", Content: "Follow Go conventions."},
		},
	}

	if err := writeContextFiles(dir, "hermes", ctx); err != nil {
		t.Fatalf("writeContextFiles failed: %v", err)
	}

	// Skills should be in the fallback .agent_context/skills/ path since
	// Hermes has no native skills discovery directory.
	skillMd, err := os.ReadFile(filepath.Join(dir, ".agent_context", "skills", "go-conventions", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read .agent_context/skills/go-conventions/SKILL.md: %v", err)
	}
	if !strings.Contains(string(skillMd), "Follow Go conventions.") {
		t.Error("SKILL.md missing content")
	}
}

func TestPrepareCodexHomeSeedsFromShared(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	// Create a fake shared codex home.
	sharedHome := t.TempDir()
	os.WriteFile(filepath.Join(sharedHome, "auth.json"), []byte(`{"token":"secret"}`), 0o644)
	os.WriteFile(filepath.Join(sharedHome, "config.json"), []byte(`{"model":"o3"}`), 0o644)
	os.WriteFile(filepath.Join(sharedHome, "config.toml"), []byte(`model = "o3"`), 0o644)
	os.WriteFile(filepath.Join(sharedHome, "instructions.md"), []byte("Be helpful."), 0o644)
	sharedPluginCache := filepath.Join(sharedHome, "plugins", "cache")
	if err := os.MkdirAll(filepath.Join(sharedPluginCache, "superpowers"), 0o755); err != nil {
		t.Fatalf("create shared plugin cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sharedPluginCache, "superpowers", "SKILL.md"), []byte("Use superpowers."), 0o644); err != nil {
		t.Fatalf("write shared plugin skill: %v", err)
	}

	// Point CODEX_HOME to our fake shared home.
	t.Setenv("CODEX_HOME", sharedHome)

	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := prepareCodexHome(codexHome, testLogger()); err != nil {
		t.Fatalf("prepareCodexHome failed: %v", err)
	}

	// sessions should be a symlink to the shared sessions dir.
	sessionsPath := filepath.Join(codexHome, "sessions")
	fi, err := os.Lstat(sessionsPath)
	if err != nil {
		t.Fatalf("sessions not found: %v", err)
	}
	sessionsIsLink := fi.Mode()&os.ModeSymlink != 0
	if !sessionsIsLink && runtime.GOOS != "windows" {
		t.Error("sessions should be a symlink")
	}
	if sessionsIsLink {
		sessTarget, _ := os.Readlink(sessionsPath)
		if sessTarget != filepath.Join(sharedHome, "sessions") {
			t.Errorf("sessions symlink target = %q, want %q", sessTarget, filepath.Join(sharedHome, "sessions"))
		}
	} else if fi.IsDir() {
		if _, err := os.Stat(sessionsPath); err != nil {
			t.Fatalf("sessions link target should be accessible: %v", err)
		}
	}

	// auth.json should be a symlink.
	authPath := filepath.Join(codexHome, "auth.json")
	fi, err = os.Lstat(authPath)
	if err != nil {
		t.Fatalf("auth.json not found: %v", err)
	}
	authIsLink := fi.Mode()&os.ModeSymlink != 0
	if !authIsLink && runtime.GOOS != "windows" {
		t.Error("auth.json should be a symlink")
	}
	if authIsLink {
		target, _ := os.Readlink(authPath)
		if target != filepath.Join(sharedHome, "auth.json") {
			t.Errorf("auth.json symlink target = %q, want %q", target, filepath.Join(sharedHome, "auth.json"))
		}
	}
	// Verify content is accessible through symlink.
	data, _ := os.ReadFile(authPath)
	if string(data) != `{"token":"secret"}` {
		t.Errorf("auth.json content = %q", data)
	}

	// config.json should be a copy (not symlink).
	configPath := filepath.Join(codexHome, "config.json")
	fi, err = os.Lstat(configPath)
	if err != nil {
		t.Fatalf("config.json not found: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("config.json should be a copy, not a symlink")
	}
	data, _ = os.ReadFile(configPath)
	if string(data) != `{"model":"o3"}` {
		t.Errorf("config.json content = %q", data)
	}

	// config.toml should be copied and have network access appended.
	data, _ = os.ReadFile(filepath.Join(codexHome, "config.toml"))
	tomlStr := string(data)
	if !strings.Contains(tomlStr, `model = "o3"`) {
		t.Errorf("config.toml missing original model setting, got: %q", tomlStr)
	}
	if !strings.Contains(tomlStr, "network_access = true") {
		t.Errorf("config.toml missing network_access, got: %q", tomlStr)
	}

	// instructions.md should be copied.
	data, _ = os.ReadFile(filepath.Join(codexHome, "instructions.md"))
	if string(data) != "Be helpful." {
		t.Errorf("instructions.md content = %q", data)
	}

	// plugin cache should be exposed at the same relative path in codex-home.
	pluginSkillPath := filepath.Join(codexHome, "plugins", "cache", "superpowers", "SKILL.md")
	data, err = os.ReadFile(pluginSkillPath)
	if err != nil {
		t.Fatalf("plugin cache skill not exposed: %v", err)
	}
	if string(data) != "Use superpowers." {
		t.Errorf("plugin cache skill content = %q", data)
	}
}

// Regression test for #1753 — Codex Desktop writes plugin-backed
// `[[skills.config]]` entries without a `path` field, and the CLI's TOML
// parser rejects them with `missing field path`. prepareCodexHome must drop
// every `[[skills.config]]` entry while copying the user's config.toml so
// the per-task home stays parseable.
func TestPrepareCodexHomeStripsSkillsConfigEntries(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	sharedConfig := `model = "o3"

[[skills.config]]
path = "/Users/x/SKILL.md"
enabled = false

[[skills.config]]
name = "superpowers:brainstorming"
enabled = false

[profiles.default]
model = "o3"
`
	if err := os.WriteFile(filepath.Join(sharedHome, "config.toml"), []byte(sharedConfig), 0o644); err != nil {
		t.Fatalf("write shared config.toml: %v", err)
	}
	t.Setenv("CODEX_HOME", sharedHome)

	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := prepareCodexHome(codexHome, testLogger()); err != nil {
		t.Fatalf("prepareCodexHome failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read per-task config.toml: %v", err)
	}
	tomlStr := string(data)
	if strings.Contains(tomlStr, "[[skills.config]]") {
		t.Errorf("per-task config.toml should not inherit [[skills.config]] entries, got:\n%s", tomlStr)
	}
	if strings.Contains(tomlStr, "superpowers:brainstorming") {
		t.Errorf("per-task config.toml should not retain plugin skill names, got:\n%s", tomlStr)
	}
	if !strings.Contains(tomlStr, `model = "o3"`) {
		t.Errorf("top-level keys should be preserved, got:\n%s", tomlStr)
	}
	if !strings.Contains(tomlStr, "[profiles.default]") {
		t.Errorf("unrelated tables should be preserved, got:\n%s", tomlStr)
	}
}

func TestPrepareCodexHomeSkipsMissingFiles(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	// Empty shared home — no files to seed.
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := prepareCodexHome(codexHome, testLogger()); err != nil {
		t.Fatalf("prepareCodexHome failed: %v", err)
	}

	// Directory should contain sessions symlink + auto-generated config.toml.
	entries, err := os.ReadDir(codexHome)
	if err != nil {
		t.Fatalf("failed to read codex-home: %v", err)
	}
	entryNames := make(map[string]bool, len(entries))
	for _, e := range entries {
		entryNames[e.Name()] = true
	}
	if !entryNames["sessions"] {
		t.Error("expected sessions symlink")
	}
	if !entryNames["config.toml"] {
		t.Error("expected config.toml (auto-generated for network access)")
	}
	if !entryNames["plugins"] {
		t.Error("expected plugins directory for plugin cache exposure")
	}
	for name := range entryNames {
		if name != "sessions" && name != "config.toml" && name != "plugins" {
			t.Errorf("unexpected entry: %s", name)
		}
	}
	// sessions should be a symlink to the shared sessions dir.
	sessionsPath := filepath.Join(codexHome, "sessions")
	fi, err := os.Lstat(sessionsPath)
	if err != nil {
		t.Fatalf("sessions not found: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 && runtime.GOOS != "windows" {
		t.Error("sessions should be a symlink")
	}
	if _, err := os.Stat(filepath.Join(codexHome, "plugins", "cache")); err != nil {
		t.Fatalf("missing shared plugin cache exposure should still be tolerated and created: %v", err)
	}
}

// Regression for issue #2081: when the per-task auth.json is a stale regular
// file (e.g. left behind from an earlier Windows copy fallback), a subsequent
// Reuse() / prepareCodexHome must refresh it from the shared source rather
// than preserve the stale copy. Without this, Codex would keep retrying with
// a refresh token the OAuth server has already revoked, surfacing as
// `refresh_token_reused` / `token_expired` until the user manually nukes the
// workspace directory.
func TestPrepareCodexHome_RefreshesStaleAuthCopyOnReuse(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	os.WriteFile(filepath.Join(sharedHome, "auth.json"), []byte(`{"refresh_token":"v1"}`), 0o644)
	t.Setenv("CODEX_HOME", sharedHome)

	codexHome := filepath.Join(t.TempDir(), "codex-home")

	// Pre-seed the per-task home with a stale regular-file auth.json,
	// simulating a previous run where os.Symlink failed and createFileLink
	// fell back to copying.
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("mkdir codex-home: %v", err)
	}
	stalePath := filepath.Join(codexHome, "auth.json")
	if err := os.WriteFile(stalePath, []byte(`{"refresh_token":"v0_stale"}`), 0o644); err != nil {
		t.Fatalf("seed stale auth: %v", err)
	}

	// Shared source rotates to v2 while the per-task copy is still stuck on v0.
	os.WriteFile(filepath.Join(sharedHome, "auth.json"), []byte(`{"refresh_token":"v2"}`), 0o644)

	if err := prepareCodexHome(codexHome, testLogger()); err != nil {
		t.Fatalf("prepareCodexHome failed: %v", err)
	}

	// After Reuse, dst should mirror the current shared source — either as a
	// fresh symlink (preferred) or as a fresh copy (Windows fallback).
	data, err := os.ReadFile(stalePath)
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}
	if string(data) != `{"refresh_token":"v2"}` {
		t.Errorf("auth.json content = %q, want refreshed v2 contents", data)
	}
}

func TestEnsureCodexSandboxConfigCreatesDefaultLinux(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	policy := codexSandboxPolicyFor("linux", "0.121.0")
	if err := ensureCodexSandboxConfig(configPath, policy, "0.121.0", testLogger()); err != nil {
		t.Fatalf("ensureCodexSandboxConfig failed: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config.toml: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, multicaManagedBeginMarker) || !strings.Contains(s, multicaManagedEndMarker) {
		t.Errorf("missing managed block markers, got:\n%s", s)
	}
	if !strings.Contains(s, `sandbox_mode = "workspace-write"`) {
		t.Error("missing sandbox_mode")
	}
	// The managed block uses TOML dotted-key form rather than a
	// `[sandbox_workspace_write]` section header so it cannot leak into or
	// inherit from any surrounding table scope. See upsertMulticaManagedBlock
	// for why.
	if strings.Contains(s, "[sandbox_workspace_write]") {
		t.Errorf("managed block must not open a [sandbox_workspace_write] table header, got:\n%s", s)
	}
	if !strings.Contains(s, "sandbox_workspace_write.network_access = true") {
		t.Errorf("missing dotted-key network_access = true, got:\n%s", s)
	}
}

func TestEnsureCodexSandboxConfigDarwinFallsBack(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	policy := codexSandboxPolicyFor("darwin", "0.121.0")
	if err := ensureCodexSandboxConfig(configPath, policy, "0.121.0", testLogger()); err != nil {
		t.Fatalf("ensureCodexSandboxConfig failed: %v", err)
	}

	s, _ := os.ReadFile(configPath)
	if !strings.Contains(string(s), `sandbox_mode = "danger-full-access"`) {
		t.Errorf("expected danger-full-access fallback on macOS, got:\n%s", s)
	}
	if strings.Contains(string(s), "[sandbox_workspace_write]") {
		t.Errorf("should not emit workspace-write section on macOS fallback, got:\n%s", s)
	}
}

func TestEnsureCodexSandboxConfigIsIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	policy := codexSandboxPolicyFor("linux", "0.121.0")
	for i := 0; i < 3; i++ {
		if err := ensureCodexSandboxConfig(configPath, policy, "0.121.0", testLogger()); err != nil {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	data, _ := os.ReadFile(configPath)
	// The managed block should appear exactly once.
	if n := strings.Count(string(data), multicaManagedBeginMarker); n != 1 {
		t.Errorf("expected exactly 1 managed block, got %d in:\n%s", n, data)
	}
}

func TestEnsureCodexSandboxConfigPreservesUserContent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	existing := `model = "o3"
approval_policy = "on-failure"
`
	os.WriteFile(configPath, []byte(existing), 0o644)

	policy := codexSandboxPolicyFor("linux", "0.121.0")
	if err := ensureCodexSandboxConfig(configPath, policy, "0.121.0", testLogger()); err != nil {
		t.Fatalf("ensureCodexSandboxConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	s := string(data)
	if !strings.Contains(s, `model = "o3"`) {
		t.Error("lost existing model setting")
	}
	if !strings.Contains(s, "approval_policy") {
		t.Error("lost existing approval_policy")
	}
	if !strings.Contains(s, "network_access = true") {
		t.Error("missing network_access = true")
	}
}

func TestEnsureCodexSandboxConfigStripsLegacyInlineDirectives(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	// Simulate a config.toml produced by an older daemon version that wrote
	// sandbox directives inline (no managed block markers). After migration,
	// the inline directives should be gone and only the managed block should
	// carry them.
	existing := `model = "o3"
sandbox_mode = "workspace-write"

[sandbox_workspace_write]
network_access = true
`
	os.WriteFile(configPath, []byte(existing), 0o644)

	policy := codexSandboxPolicyFor("darwin", "0.121.0")
	if err := ensureCodexSandboxConfig(configPath, policy, "0.121.0", testLogger()); err != nil {
		t.Fatalf("ensureCodexSandboxConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	s := string(data)
	if !strings.Contains(s, `model = "o3"`) {
		t.Error("should have preserved unrelated user config")
	}
	// Inline sandbox_mode and [sandbox_workspace_write] should be stripped.
	if strings.Count(s, "sandbox_mode") != 1 {
		t.Errorf("expected exactly one sandbox_mode line (inside managed block), got:\n%s", s)
	}
	if strings.Contains(s, "[sandbox_workspace_write]") {
		t.Errorf("darwin fallback should not retain workspace-write section:\n%s", s)
	}
	if !strings.Contains(s, `sandbox_mode = "danger-full-access"`) {
		t.Errorf("expected danger-full-access on macOS, got:\n%s", s)
	}
}

func TestEnsureCodexSandboxConfigHoistsAboveUserTables(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	// User config that ends inside a table. If the managed block were
	// appended at EOF, `sandbox_mode = "..."` would be parsed as
	// permissions.multica.sandbox_mode and Codex would never see it — see
	// review of MUL-963 PR #1246. The block must be hoisted above any
	// user-defined table headers so it lives at the TOML root.
	existing := `model = "o3"

[permissions.multica]
trust = "always"
`
	os.WriteFile(configPath, []byte(existing), 0o644)

	policy := codexSandboxPolicyFor("linux", "0.121.0")
	if err := ensureCodexSandboxConfig(configPath, policy, "0.121.0", testLogger()); err != nil {
		t.Fatalf("ensureCodexSandboxConfig failed: %v", err)
	}

	data, _ := os.ReadFile(configPath)
	s := string(data)

	beginIdx := strings.Index(s, multicaManagedBeginMarker)
	endIdx := strings.Index(s, multicaManagedEndMarker)
	tableIdx := strings.Index(s, "[permissions.multica]")
	if beginIdx < 0 || endIdx < 0 || tableIdx < 0 {
		t.Fatalf("expected managed block and user table to both be present, got:\n%s", s)
	}
	// The entire managed block must sit before the user's table header so
	// that sandbox_mode and sandbox_workspace_write.network_access are
	// parsed at the TOML root.
	if !(beginIdx < endIdx && endIdx < tableIdx) {
		t.Errorf("managed block must be hoisted above [permissions.multica]; got begin=%d end=%d table=%d:\n%s", beginIdx, endIdx, tableIdx, s)
	}
	// User content must be preserved verbatim.
	if !strings.Contains(s, `model = "o3"`) {
		t.Error("lost user top-level key")
	}
	if !strings.Contains(s, `trust = "always"`) {
		t.Error("lost user permissions.multica content")
	}

	// Running again must be idempotent even when the preceding content ends
	// inside a table.
	if err := ensureCodexSandboxConfig(configPath, policy, "0.121.0", testLogger()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	data2, _ := os.ReadFile(configPath)
	if string(data2) != s {
		t.Errorf("second pass should be idempotent:\n--- first ---\n%s\n--- second ---\n%s", s, data2)
	}
	if n := strings.Count(string(data2), multicaManagedBeginMarker); n != 1 {
		t.Errorf("expected exactly one managed block after idempotent rewrite, got %d", n)
	}
}

func TestEnsureCodexSandboxConfigMovesLegacyTrailingBlockToTop(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	// Simulate a config.toml produced by the pre-fix PR #1246 logic, which
	// appended the managed block to EOF — so the block sits below a user
	// table. On the next daemon run, the block must be hoisted back to the
	// top; otherwise sandbox_mode remains trapped inside the preceding table.
	legacy := `model = "o3"

[permissions.multica]
trust = "always"

` + multicaManagedBeginMarker + `
sandbox_mode = "workspace-write"

[sandbox_workspace_write]
network_access = true
` + multicaManagedEndMarker + `
`
	os.WriteFile(configPath, []byte(legacy), 0o644)

	policy := codexSandboxPolicyFor("linux", "0.121.0")
	if err := ensureCodexSandboxConfig(configPath, policy, "0.121.0", testLogger()); err != nil {
		t.Fatalf("ensureCodexSandboxConfig failed: %v", err)
	}
	data, _ := os.ReadFile(configPath)
	s := string(data)

	beginIdx := strings.Index(s, multicaManagedBeginMarker)
	tableIdx := strings.Index(s, "[permissions.multica]")
	if beginIdx < 0 || tableIdx < 0 || beginIdx > tableIdx {
		t.Errorf("expected managed block to be hoisted above [permissions.multica], got:\n%s", s)
	}
	if strings.Count(s, multicaManagedBeginMarker) != 1 {
		t.Errorf("expected exactly one managed block, got:\n%s", s)
	}
	// The old inline `[sandbox_workspace_write]` header must be gone — the
	// new block uses dotted-key form only.
	if strings.Contains(s, "[sandbox_workspace_write]") {
		t.Errorf("managed block must not emit [sandbox_workspace_write] table header, got:\n%s", s)
	}
}

func TestCodexSandboxPolicyFor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		goos     string
		version  string
		wantMode string
		wantNet  bool
	}{
		{"linux any version", "linux", "0.100.0", "workspace-write", true},
		{"linux unknown version", "linux", "", "workspace-write", true},
		{"darwin old version", "darwin", "0.121.0", "danger-full-access", false},
		{"darwin unknown version", "darwin", "", "danger-full-access", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := codexSandboxPolicyFor(tc.goos, tc.version)
			if p.Mode != tc.wantMode {
				t.Errorf("mode = %q, want %q", p.Mode, tc.wantMode)
			}
			if p.NetworkAccess != tc.wantNet {
				t.Errorf("network_access = %v, want %v", p.NetworkAccess, tc.wantNet)
			}
			if p.Reason == "" {
				t.Error("expected non-empty Reason")
			}
		})
	}
}

func TestPrepareCodexHomeEnsuresNetworkAccess(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	// Empty shared home — no config.toml to copy.
	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	codexHome := filepath.Join(t.TempDir(), "codex-home")
	// Default prepareCodexHome assumes linux-like behavior.
	if err := prepareCodexHome(codexHome, testLogger()); err != nil {
		t.Fatalf("prepareCodexHome failed: %v", err)
	}

	// config.toml should be created with network access defaults.
	data, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("config.toml not created: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "network_access = true") {
		t.Error("config.toml missing network_access = true")
	}
	if !strings.Contains(s, `sandbox_mode = "workspace-write"`) {
		t.Error("config.toml missing sandbox_mode")
	}
}

func TestReuseRestoresCodexHome(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	workspacesRoot := t.TempDir()

	// First, Prepare a codex env.
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-codex-reuse",
		TaskID:         "e5f6a7b8-c9d0-1234-efab-567890123456",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "reuse-test"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if env.CodexHome == "" {
		t.Fatal("expected CodexHome to be set after Prepare")
	}

	// Reuse should restore CodexHome.
	reused := Reuse(env.WorkDir, "codex", "", TaskContextForEnv{IssueID: "reuse-test"}, testLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil")
	}
	if reused.CodexHome == "" {
		t.Fatal("expected CodexHome to be restored after Reuse")
	}

	// Verify config.toml has a managed block (exact mode depends on host
	// platform; either workspace-write or danger-full-access is valid).
	data, err := os.ReadFile(filepath.Join(reused.CodexHome, "config.toml"))
	if err != nil {
		t.Fatalf("config.toml not found in reused CodexHome: %v", err)
	}
	if !strings.Contains(string(data), multicaManagedBeginMarker) {
		t.Error("reused config.toml missing multica-managed block")
	}
}

func TestReuseRestoresCodexPluginCache(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	sharedPluginCache := filepath.Join(sharedHome, "plugins", "cache")
	if err := os.MkdirAll(filepath.Join(sharedPluginCache, "superpowers"), 0o755); err != nil {
		t.Fatalf("create shared plugin cache: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sharedPluginCache, "superpowers", "SKILL.md"), []byte("Use superpowers."), 0o644); err != nil {
		t.Fatalf("write shared plugin skill: %v", err)
	}
	t.Setenv("CODEX_HOME", sharedHome)

	workspacesRoot := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-codex-plugin-reuse",
		TaskID:         "a5f6a7b8-c9d0-1234-efab-567890123456",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "reuse-plugin-test"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if err := os.RemoveAll(filepath.Join(env.CodexHome, "plugins")); err != nil {
		t.Fatalf("remove codex plugins dir: %v", err)
	}

	reused := Reuse(env.WorkDir, "codex", "", TaskContextForEnv{IssueID: "reuse-plugin-test"}, testLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil")
	}

	data, err := os.ReadFile(filepath.Join(reused.CodexHome, "plugins", "cache", "superpowers", "SKILL.md"))
	if err != nil {
		t.Fatalf("reused codex plugin cache not restored: %v", err)
	}
	if string(data) != "Use superpowers." {
		t.Errorf("reused plugin cache skill content = %q", data)
	}
}

func TestReuseWritesMissingCodexWorkspaceSkills(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	workspacesRoot := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-codex-skill-reuse",
		TaskID:         "b5f6a7b8-c9d0-1234-efab-567890123456",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "reuse-skill-test"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if err := os.RemoveAll(filepath.Join(env.CodexHome, "skills")); err != nil {
		t.Fatalf("remove codex skills dir: %v", err)
	}

	reused := Reuse(env.WorkDir, "codex", "", TaskContextForEnv{
		IssueID: "reuse-skill-test",
		AgentSkills: []SkillContextForEnv{
			{
				Name:    "Writing",
				Content: "Write clearly.",
				Files:   []SkillFileContextForEnv{{Path: "examples/example.md", Content: "Example"}},
			},
		},
	}, testLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil")
	}

	data, err := os.ReadFile(filepath.Join(reused.CodexHome, "skills", "writing", "SKILL.md"))
	if err != nil {
		t.Fatalf("missing reused codex workspace skill: %v", err)
	}
	if string(data) != "Write clearly." {
		t.Errorf("skill content = %q", data)
	}
	example, err := os.ReadFile(filepath.Join(reused.CodexHome, "skills", "writing", "examples", "example.md"))
	if err != nil {
		t.Fatalf("missing reused codex workspace skill support file: %v", err)
	}
	if string(example) != "Example" {
		t.Errorf("support file content = %q", example)
	}
}

func TestReuseUpdatesCodexWorkspaceSkills(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	workspacesRoot := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-codex-skill-update",
		TaskID:         "c5f6a7b8-c9d0-1234-efab-567890123456",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task: TaskContextForEnv{
			IssueID: "reuse-skill-update-test",
			AgentSkills: []SkillContextForEnv{
				{
					Name:    "Writing",
					Content: "Old writing guidance.",
					Files:   []SkillFileContextForEnv{{Path: "examples/example.md", Content: "Old example"}},
				},
			},
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	reused := Reuse(env.WorkDir, "codex", "", TaskContextForEnv{
		IssueID: "reuse-skill-update-test",
		AgentSkills: []SkillContextForEnv{
			{
				Name:    "Writing",
				Content: "Updated writing guidance.",
				Files:   []SkillFileContextForEnv{{Path: "examples/example.md", Content: "Updated example"}},
			},
		},
	}, testLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil")
	}

	data, err := os.ReadFile(filepath.Join(reused.CodexHome, "skills", "writing", "SKILL.md"))
	if err != nil {
		t.Fatalf("missing reused codex workspace skill: %v", err)
	}
	if string(data) != "Updated writing guidance." {
		t.Errorf("skill content = %q", data)
	}
	example, err := os.ReadFile(filepath.Join(reused.CodexHome, "skills", "writing", "examples", "example.md"))
	if err != nil {
		t.Fatalf("missing reused codex workspace skill support file: %v", err)
	}
	if string(example) != "Updated example" {
		t.Errorf("support file content = %q", example)
	}
}

// TestPrepareCodexSeedsUserSkills covers the fix for #1922: skills the user
// installs under ~/.codex/skills/ must be discoverable by the codex CLI
// inside a Multica task, despite the daemon redirecting CODEX_HOME to a
// per-task directory.
func TestPrepareCodexSeedsUserSkills(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	// Lay out two user-installed skills with both a SKILL.md and a
	// supporting file, plus an ignored dotfile that must not be copied.
	userSkills := filepath.Join(sharedHome, "skills")
	if err := os.MkdirAll(filepath.Join(userSkills, "summarize", "examples"), 0o755); err != nil {
		t.Fatalf("seed user skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userSkills, "summarize", "SKILL.md"), []byte("summarize"), 0o644); err != nil {
		t.Fatalf("seed user SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userSkills, "summarize", "examples", "ex.md"), []byte("example"), 0o644); err != nil {
		t.Fatalf("seed user support file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(userSkills, "translate"), 0o755); err != nil {
		t.Fatalf("seed second user skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userSkills, "translate", "SKILL.md"), []byte("translate"), 0o644); err != nil {
		t.Fatalf("seed second user SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userSkills, ".DS_Store"), []byte("noise"), 0o644); err != nil {
		t.Fatalf("seed ignored dotfile: %v", err)
	}

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-user-skills",
		TaskID:         "d6f7a8b9-c0d1-2345-efab-678901234567",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "user-skills-test"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if data, err := os.ReadFile(filepath.Join(env.CodexHome, "skills", "summarize", "SKILL.md")); err != nil {
		t.Fatalf("user skill SKILL.md not seeded: %v", err)
	} else if string(data) != "summarize" {
		t.Errorf("summarize SKILL.md = %q, want %q", data, "summarize")
	}
	if data, err := os.ReadFile(filepath.Join(env.CodexHome, "skills", "summarize", "examples", "ex.md")); err != nil {
		t.Fatalf("user skill support file not seeded: %v", err)
	} else if string(data) != "example" {
		t.Errorf("ex.md = %q, want %q", data, "example")
	}
	if data, err := os.ReadFile(filepath.Join(env.CodexHome, "skills", "translate", "SKILL.md")); err != nil {
		t.Fatalf("second user skill not seeded: %v", err)
	} else if string(data) != "translate" {
		t.Errorf("translate SKILL.md = %q, want %q", data, "translate")
	}
	if _, err := os.Stat(filepath.Join(env.CodexHome, "skills", ".DS_Store")); !os.IsNotExist(err) {
		t.Errorf("ignored dotfile leaked into codex-home/skills: err=%v", err)
	}
}

// TestPrepareCodexWorkspaceSkillBeatsUserSkillOnConflict checks that when a
// workspace-assigned skill shares a sanitized name with a user-installed
// skill, the workspace version fully replaces the user version (rather than
// leaving stale user files lingering).
func TestPrepareCodexWorkspaceSkillBeatsUserSkillOnConflict(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	userSkillDir := filepath.Join(sharedHome, "skills", "writing")
	if err := os.MkdirAll(filepath.Join(userSkillDir, "drafts"), 0o755); err != nil {
		t.Fatalf("seed user writing skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userSkillDir, "SKILL.md"), []byte("user writing"), 0o644); err != nil {
		t.Fatalf("seed user SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userSkillDir, "drafts", "stale.md"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed user stale file: %v", err)
	}

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-skill-conflict",
		TaskID:         "e7f8a9b0-c1d2-3456-efab-789012345678",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task: TaskContextForEnv{
			IssueID: "skill-conflict-test",
			AgentSkills: []SkillContextForEnv{
				{Name: "Writing", Content: "workspace writing"},
			},
		},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	data, err := os.ReadFile(filepath.Join(env.CodexHome, "skills", "writing", "SKILL.md"))
	if err != nil {
		t.Fatalf("workspace skill not written: %v", err)
	}
	if string(data) != "workspace writing" {
		t.Errorf("SKILL.md = %q, want workspace content", data)
	}
	// The user's stale support file must not leak through — seeding is
	// skipped entirely for names that workspace skills claim.
	if _, err := os.Stat(filepath.Join(env.CodexHome, "skills", "writing", "drafts", "stale.md")); !os.IsNotExist(err) {
		t.Errorf("user-skill stale file leaked despite workspace conflict: err=%v", err)
	}
}

// TestPrepareCodexNoUserSkillsDir is a regression guard for the empty case —
// when ~/.codex/skills doesn't exist, the seed step is a no-op and Prepare
// still succeeds.
func TestPrepareCodexNoUserSkillsDir(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-no-user-skills",
		TaskID:         "f8a9b0c1-d2e3-4567-fabc-890123456789",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "no-user-skills-test"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)
	if _, err := os.Stat(filepath.Join(env.CodexHome, "skills")); !os.IsNotExist(err) {
		t.Errorf("skills dir should not exist when neither user nor workspace skills are present, err=%v", err)
	}
}

// TestPrepareCodexResolvesUserSkillSymlinks covers the lark-cli /
// shared-installer case: each user skill is a symlink into a separate
// installer directory. The per-task home must end up with a real copy, not
// a dangling symlink that points outside the task root.
func TestPrepareCodexResolvesUserSkillSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on Windows; covered by Unix path")
	}
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	installerRoot := filepath.Join(t.TempDir(), "installer", "lark-mail")
	if err := os.MkdirAll(installerRoot, 0o755); err != nil {
		t.Fatalf("seed installer dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installerRoot, "SKILL.md"), []byte("lark"), 0o644); err != nil {
		t.Fatalf("seed installer SKILL.md: %v", err)
	}

	userSkills := filepath.Join(sharedHome, "skills")
	if err := os.MkdirAll(userSkills, 0o755); err != nil {
		t.Fatalf("seed user skills dir: %v", err)
	}
	if err := os.Symlink(installerRoot, filepath.Join(userSkills, "lark-mail")); err != nil {
		t.Fatalf("seed user skill symlink: %v", err)
	}

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-symlinked-skills",
		TaskID:         "a9b0c1d2-e3f4-5678-abcd-901234567890",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "symlinked-skills-test"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	dst := filepath.Join(env.CodexHome, "skills", "lark-mail")
	fi, err := os.Lstat(dst)
	if err != nil {
		t.Fatalf("seeded skill missing: %v", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		t.Errorf("seeded skill should be a real directory, got a symlink")
	}
	data, err := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if err != nil {
		t.Fatalf("seeded SKILL.md missing: %v", err)
	}
	if string(data) != "lark" {
		t.Errorf("seeded SKILL.md = %q, want %q", data, "lark")
	}
}

// TestReuseSeedsUserSkillUpdates ensures that user-skill edits between two
// runs of the same task (the Reuse path) propagate into the per-task home.
func TestReuseSeedsUserSkillUpdates(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	userSkill := filepath.Join(sharedHome, "skills", "summarize")
	if err := os.MkdirAll(userSkill, 0o755); err != nil {
		t.Fatalf("seed user skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userSkill, "SKILL.md"), []byte("v1"), 0o644); err != nil {
		t.Fatalf("seed v1 SKILL.md: %v", err)
	}

	workspacesRoot := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: workspacesRoot,
		WorkspaceID:    "ws-user-skill-reuse",
		TaskID:         "b0c1d2e3-f4a5-6789-abcd-012345678901",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "user-skill-reuse-test"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if err := os.WriteFile(filepath.Join(userSkill, "SKILL.md"), []byte("v2"), 0o644); err != nil {
		t.Fatalf("update user SKILL.md: %v", err)
	}

	reused := Reuse(env.WorkDir, "codex", "", TaskContextForEnv{
		IssueID: "user-skill-reuse-test",
	}, testLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil")
	}
	data, err := os.ReadFile(filepath.Join(reused.CodexHome, "skills", "summarize", "SKILL.md"))
	if err != nil {
		t.Fatalf("user skill not refreshed on reuse: %v", err)
	}
	if string(data) != "v2" {
		t.Errorf("after Reuse, user skill content = %q, want %q", data, "v2")
	}
}

// TestReuseClearsUserSkillResidueOnWorkspaceConflict locks in the fix for
// the GPT-Boy review on PR #2519: when round 1 seeded a user skill named
// `writing` (including support files) and round 2 reuses the same workdir
// with a workspace skill `Writing`, the user-version support files must not
// linger under the workspace skill's directory.
func TestReuseClearsUserSkillResidueOnWorkspaceConflict(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	userSkillDir := filepath.Join(sharedHome, "skills", "writing")
	if err := os.MkdirAll(filepath.Join(userSkillDir, "drafts"), 0o755); err != nil {
		t.Fatalf("seed user skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userSkillDir, "SKILL.md"), []byte("user writing"), 0o644); err != nil {
		t.Fatalf("seed user SKILL.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userSkillDir, "drafts", "stale.md"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("seed user support file: %v", err)
	}

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-reuse-conflict",
		TaskID:         "c1d2e3f4-a5b6-7890-abcd-123456789012",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "reuse-conflict-test"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	// Round 1 had no workspace skill, so the user version should be present.
	if _, err := os.Stat(filepath.Join(env.CodexHome, "skills", "writing", "drafts", "stale.md")); err != nil {
		t.Fatalf("user support file should be seeded in round 1: %v", err)
	}

	reused := Reuse(env.WorkDir, "codex", "", TaskContextForEnv{
		IssueID: "reuse-conflict-test",
		AgentSkills: []SkillContextForEnv{
			{Name: "Writing", Content: "workspace writing"},
		},
	}, testLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil")
	}

	data, err := os.ReadFile(filepath.Join(reused.CodexHome, "skills", "writing", "SKILL.md"))
	if err != nil {
		t.Fatalf("workspace SKILL.md missing after reuse: %v", err)
	}
	if string(data) != "workspace writing" {
		t.Errorf("SKILL.md = %q, want workspace content", data)
	}
	if _, err := os.Stat(filepath.Join(reused.CodexHome, "skills", "writing", "drafts", "stale.md")); !os.IsNotExist(err) {
		t.Errorf("round-1 user support file leaked into round-2 workspace skill dir, err=%v", err)
	}
}

// TestReuseClearsRemovedUserSkill checks that uninstalling a user skill
// between two runs (delete it from ~/.codex/skills) also drops it from the
// per-task home on Reuse — otherwise users would still see deleted skills
// surface to the codex CLI.
func TestReuseClearsRemovedUserSkill(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	sharedHome := t.TempDir()
	t.Setenv("CODEX_HOME", sharedHome)

	userSkill := filepath.Join(sharedHome, "skills", "deprecated")
	if err := os.MkdirAll(userSkill, 0o755); err != nil {
		t.Fatalf("seed user skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userSkill, "SKILL.md"), []byte("deprecated"), 0o644); err != nil {
		t.Fatalf("seed user SKILL.md: %v", err)
	}

	env, err := Prepare(PrepareParams{
		WorkspacesRoot: t.TempDir(),
		WorkspaceID:    "ws-reuse-remove",
		TaskID:         "d2e3f4a5-b6c7-8901-abcd-234567890123",
		AgentName:      "Codex Agent",
		Provider:       "codex",
		Task:           TaskContextForEnv{IssueID: "reuse-remove-test"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	defer env.Cleanup(true)

	if _, err := os.Stat(filepath.Join(env.CodexHome, "skills", "deprecated", "SKILL.md")); err != nil {
		t.Fatalf("user skill should be seeded in round 1: %v", err)
	}

	// Uninstall the user skill before round 2.
	if err := os.RemoveAll(userSkill); err != nil {
		t.Fatalf("remove user skill: %v", err)
	}

	reused := Reuse(env.WorkDir, "codex", "", TaskContextForEnv{
		IssueID: "reuse-remove-test",
	}, testLogger())
	if reused == nil {
		t.Fatal("Reuse returned nil")
	}
	if _, err := os.Stat(filepath.Join(reused.CodexHome, "skills", "deprecated")); !os.IsNotExist(err) {
		t.Errorf("removed user skill still present in per-task home after reuse, err=%v", err)
	}
}

func TestEnsureSymlinkRepairsBrokenLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "source.json")
	dst := filepath.Join(dir, "link.json")

	os.WriteFile(src, []byte("real"), 0o644)

	// Create a broken symlink pointing to a non-existent file.
	if err := os.Symlink(filepath.Join(dir, "old-source.json"), dst); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("file symlink unavailable on this Windows session: %v", err)
		}
		t.Fatalf("seed broken symlink: %v", err)
	}

	if err := ensureSymlink(src, dst); err != nil {
		t.Fatalf("ensureSymlink failed: %v", err)
	}

	// Should now point to src.
	target, _ := os.Readlink(dst)
	if target != src {
		t.Errorf("symlink target = %q, want %q", target, src)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "real" {
		t.Errorf("content = %q, want %q", data, "real")
	}
}

func TestWriteReadGCMeta(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	issueID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
	wsID := "ws-test-001"

	if err := WriteGCMeta(dir, GCMeta{
		Kind:        GCKindIssue,
		IssueID:     issueID,
		WorkspaceID: wsID,
	}, discardLogger()); err != nil {
		t.Fatalf("WriteGCMeta: %v", err)
	}

	meta, err := ReadGCMeta(dir)
	if err != nil {
		t.Fatalf("ReadGCMeta: %v", err)
	}

	if meta.Kind != GCKindIssue {
		t.Errorf("Kind = %q, want %q", meta.Kind, GCKindIssue)
	}
	if meta.IssueID != issueID {
		t.Errorf("IssueID = %q, want %q", meta.IssueID, issueID)
	}
	if meta.WorkspaceID != wsID {
		t.Errorf("WorkspaceID = %q, want %q", meta.WorkspaceID, wsID)
	}
	if meta.CompletedAt.IsZero() {
		t.Error("CompletedAt should not be zero")
	}
}

func TestWriteGCMeta_EmptyRoot(t *testing.T) {
	t.Parallel()
	if err := WriteGCMeta("", GCMeta{Kind: GCKindIssue, IssueID: "x", WorkspaceID: "ws"}, discardLogger()); err != nil {
		t.Fatalf("expected nil for empty root, got %v", err)
	}
}

func TestWriteGCMeta_EmptyKind(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := WriteGCMeta(dir, GCMeta{WorkspaceID: "ws"}, discardLogger()); err != nil {
		t.Fatalf("expected nil for empty kind, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, gcMetaFile)); !os.IsNotExist(err) {
		t.Fatalf("expected gc meta file to be absent, got err=%v", err)
	}
}

// Pre-v2 meta files lacked the kind field. ReadGCMeta must default an empty
// kind to GCKindIssue so the existing on-disk meta files keep flowing
// through the issue path.
func TestReadGCMeta_LegacyFileDefaultsToIssueKind(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	legacy := []byte(`{"issue_id":"a1b2c3d4-e5f6-7890-abcd-ef1234567890","workspace_id":"ws","completed_at":"2025-01-01T00:00:00Z"}`)
	if err := os.WriteFile(filepath.Join(dir, gcMetaFile), legacy, 0o644); err != nil {
		t.Fatal(err)
	}
	meta, err := ReadGCMeta(dir)
	if err != nil {
		t.Fatalf("ReadGCMeta: %v", err)
	}
	if meta.Kind != GCKindIssue {
		t.Fatalf("legacy kind: want %q, got %q", GCKindIssue, meta.Kind)
	}
	if meta.IssueID != "a1b2c3d4-e5f6-7890-abcd-ef1234567890" {
		t.Fatalf("legacy issue_id: got %q", meta.IssueID)
	}
}

// New v2 meta files for chat / autopilot / quick-create round-trip without
// being misclassified as the issue kind.
func TestWriteReadGCMeta_KindRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		meta GCMeta
		want GCMetaKind
	}{
		{"chat", GCMeta{Kind: GCKindChat, ChatSessionID: "cs-1", WorkspaceID: "ws"}, GCKindChat},
		{"autopilot_run", GCMeta{Kind: GCKindAutopilotRun, AutopilotRunID: "ar-1", WorkspaceID: "ws"}, GCKindAutopilotRun},
		{"quick_create", GCMeta{Kind: GCKindQuickCreate, TaskID: "t-1", WorkspaceID: "ws"}, GCKindQuickCreate},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := WriteGCMeta(dir, tc.meta, discardLogger()); err != nil {
				t.Fatalf("WriteGCMeta: %v", err)
			}
			got, err := ReadGCMeta(dir)
			if err != nil {
				t.Fatalf("ReadGCMeta: %v", err)
			}
			if got.Kind != tc.want {
				t.Fatalf("Kind: want %q, got %q", tc.want, got.Kind)
			}
		})
	}
}

func TestReadGCMeta_NoFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := ReadGCMeta(dir)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestInjectRuntimeConfigMentionLoopHardening locks in the mention-loop
// instructions (see MUL-1323 / GH#1576). Two agents were stuck in an infinite
// @mention loop because the harness told them mentions were "actions" but did
// not tell them (a) when NOT to mention, (b) that silence ends a thread, or
// (c) that the triggering comment was from another agent. If any of the
// signals below regress, agent-to-agent loops come back.
func TestInjectRuntimeConfigMentionLoopHardening(t *testing.T) {
	t.Parallel()

	commentTriggerCtx := TaskContextForEnv{
		IssueID:          "issue-1",
		TriggerCommentID: "comment-1",
	}
	assignmentCtx := TaskContextForEnv{IssueID: "issue-1"}

	readClaudeMD := func(t *testing.T, ctx TaskContextForEnv) string {
		t.Helper()
		dir := t.TempDir()
		if _, err := InjectRuntimeConfig(dir, "claude", ctx); err != nil {
			t.Fatalf("InjectRuntimeConfig failed: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
		if err != nil {
			t.Fatalf("read CLAUDE.md: %v", err)
		}
		return string(data)
	}

	t.Run("mentions-section-lists-loop-protocol", func(t *testing.T) {
		t.Parallel()
		s := readClaudeMD(t, assignmentCtx)
		for _, want := range []string{
			"side-effecting actions",
			"enqueues a new run for that agent",
			"When NOT to use a mention link",
			"When a mention IS appropriate",
			"end with no mention at all",
			"Silence ends conversations",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("Mentions section missing %q\n---\n%s", want, s)
			}
		}
	})

	t.Run("closing-line-no-longer-says-always-mention", func(t *testing.T) {
		t.Parallel()
		s := readClaudeMD(t, assignmentCtx)
		// The old footer said "**always** use the mention format" which models
		// over-generalized to agent/member mentions. Guard against regression.
		if strings.Contains(s, "**always** use the mention format") {
			t.Errorf("CLAUDE.md still contains the overreaching \"**always** use the mention format\" guidance")
		}
	})

	t.Run("workflow-carries-silence-as-exit-and-no-signoff-mention", func(t *testing.T) {
		t.Parallel()
		s := readClaudeMD(t, commentTriggerCtx)
		// The anti-loop signal for CLAUDE.md lives in the numbered workflow
		// steps (4 + 5), not in a dedicated preamble. Lock in the key phrases
		// so the signal can't decay back into pure prose again.
		for _, want := range []string{
			"Decide whether a reply is warranted",
			"Silence is a valid and preferred way",
			"Never @mention the agent you are replying to as a thank-you or sign-off",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("comment-triggered CLAUDE.md missing %q", want)
			}
		}
	})
}

// TestInjectRuntimeConfigSquadLeaderCommentTriggeredNoAction verifies that
// when IsSquadLeader is true and the task is comment-triggered, the generated
// CLAUDE.md explicitly forbids posting comments that merely announce no_action.
// This is the fix for MUL-2168 — squad leaders were posting "Exiting silently"
// comments because the comment-triggered path lacked the prohibition.
func TestInjectRuntimeConfigSquadLeaderCommentTriggeredNoAction(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	ctx := TaskContextForEnv{
		IssueID:          "issue-1",
		TriggerCommentID: "comment-1",
		IsSquadLeader:    true,
	}
	if _, err := InjectRuntimeConfig(dir, "claude", ctx); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	s := string(data)

	// The comment-triggered workflow must contain the squad leader no_action rule.
	for _, want := range []string{
		"Squad leader rule",
		"DO NOT post any comment",
		"multica squad activity",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("squad leader comment-triggered CLAUDE.md missing %q", want)
		}
	}

	// The Output section must use strong prohibition language.
	if !strings.Contains(s, "you MUST exit without posting any comment") {
		t.Errorf("Output section missing strong prohibition for squad leader no_action")
	}

	// Non-squad-leader should NOT have the squad leader rule in comment-triggered path.
	dir2 := t.TempDir()
	ctx2 := TaskContextForEnv{
		IssueID:          "issue-1",
		TriggerCommentID: "comment-1",
		IsSquadLeader:    false,
	}
	if _, err := InjectRuntimeConfig(dir2, "claude", ctx2); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}
	data2, err := os.ReadFile(filepath.Join(dir2, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	s2 := string(data2)
	if strings.Contains(s2, "Squad leader rule") {
		t.Errorf("non-squad-leader CLAUDE.md should NOT contain squad leader rule")
	}
}
