package execenv

import (
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

func TestPrepareWithRepoContext(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()

	taskCtx := TaskContextForEnv{
		IssueID: "b2c3d4e5-f6a7-8901-bcde-f12345678901",
		Repos: []RepoContextForEnv{
			{URL: "https://github.com/org/backend", Description: "Go backend"},
			{URL: "https://github.com/org/frontend", Description: "React frontend"},
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
	if err := InjectRuntimeConfig(env.WorkDir, "claude", taskCtx); err != nil {
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
		"Go backend",
		"https://github.com/org/frontend",
		"React frontend",
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

	if err := InjectRuntimeConfig(dir, "claude", ctx); err != nil {
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

func TestInjectRuntimeConfigGemini(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID:     "test-issue-id",
		AgentSkills: []SkillContextForEnv{{Name: "Writing", Content: "Write clearly."}},
	}

	if err := InjectRuntimeConfig(dir, "gemini", ctx); err != nil {
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

	if err := InjectRuntimeConfig(dir, "codex", ctx); err != nil {
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

	if err := InjectRuntimeConfig(dir, "claude", ctx); err != nil {
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

	// Skills should be in .config/opencode/skills/ (native discovery).
	skillMd, err := os.ReadFile(filepath.Join(dir, ".config", "opencode", "skills", "go-conventions", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read .config/opencode/skills/go-conventions/SKILL.md: %v", err)
	}
	if !strings.Contains(string(skillMd), "Follow Go conventions.") {
		t.Error("SKILL.md missing content")
	}

	// Supporting files should also be under .config/opencode/skills/.
	supportFile, err := os.ReadFile(filepath.Join(dir, ".config", "opencode", "skills", "go-conventions", "templates", "example.go"))
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

func TestInjectRuntimeConfigOpencode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	ctx := TaskContextForEnv{
		IssueID:     "test-issue-id",
		AgentSkills: []SkillContextForEnv{{Name: "Coding", Content: "Write good code."}},
	}

	if err := InjectRuntimeConfig(dir, "opencode", ctx); err != nil {
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

func TestPrepareWithRepoContextOpencode(t *testing.T) {
	t.Parallel()
	workspacesRoot := t.TempDir()

	taskCtx := TaskContextForEnv{
		IssueID: "c3d4e5f6-a7b8-9012-cdef-123456789012",
		Repos: []RepoContextForEnv{
			{URL: "https://github.com/org/backend", Description: "Go backend"},
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

	if err := InjectRuntimeConfig(env.WorkDir, "opencode", taskCtx); err != nil {
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
		"Go backend",
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
			if err := InjectRuntimeConfig(dir, "claude", tc.ctx); err != nil {
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

// TestInjectRuntimeConfigDirectsMultiLineWritesToStdin pins the guidance that
// any multi-line content for `multica issue comment add` must go through
// `--content-stdin` + a HEREDOC. Agents that reached for the inline
// `--content "...\n\n..."` form ended up with literal 4-char `\n` sequences
// in stored comments because bash does not expand backslash escapes inside
// double quotes; see MUL-1467. This test prevents the multi-line guidance
// from silently regressing back into a "for special characters" footnote.
func TestInjectRuntimeConfigDirectsMultiLineWritesToStdin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := InjectRuntimeConfig(dir, "claude", TaskContextForEnv{IssueID: "issue-1"}); err != nil {
		t.Fatalf("InjectRuntimeConfig failed: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read CLAUDE.md: %v", err)
	}
	s := string(data)

	for _, want := range []string{
		"multi-line content",
		"MUST pipe via stdin",
		"--content-stdin",
		"<<'COMMENT'",
		"`--description`",
		"--description-stdin",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("CLAUDE.md missing multi-line guidance %q\n---\n%s", want, s)
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

	if err := InjectRuntimeConfig(dir, "codex", ctx); err != nil {
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
	if err := InjectRuntimeConfig(dir, "unknown", TaskContextForEnv{}); err != nil {
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

	if err := InjectRuntimeConfig(dir, "hermes", ctx); err != nil {
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

	if err := WriteGCMeta(dir, issueID, wsID); err != nil {
		t.Fatalf("WriteGCMeta: %v", err)
	}

	meta, err := ReadGCMeta(dir)
	if err != nil {
		t.Fatalf("ReadGCMeta: %v", err)
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
	if err := WriteGCMeta("", "issue", "ws"); err != nil {
		t.Fatalf("expected nil for empty root, got %v", err)
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
		if err := InjectRuntimeConfig(dir, "claude", ctx); err != nil {
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
