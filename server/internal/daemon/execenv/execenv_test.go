package execenv

import (
	"log/slog"
	"os"
	"path/filepath"
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

func TestPrepareCodexHomeSeedsFromShared(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv.

	// Create a fake shared codex home.
	sharedHome := t.TempDir()
	os.WriteFile(filepath.Join(sharedHome, "auth.json"), []byte(`{"token":"secret"}`), 0o644)
	os.WriteFile(filepath.Join(sharedHome, "config.json"), []byte(`{"model":"o3"}`), 0o644)
	os.WriteFile(filepath.Join(sharedHome, "config.toml"), []byte(`model = "o3"`), 0o644)
	os.WriteFile(filepath.Join(sharedHome, "instructions.md"), []byte("Be helpful."), 0o644)

	// Point CODEX_HOME to our fake shared home.
	t.Setenv("CODEX_HOME", sharedHome)

	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := prepareCodexHome(codexHome, testLogger()); err != nil {
		t.Fatalf("prepareCodexHome failed: %v", err)
	}

	// auth.json should be a symlink.
	authPath := filepath.Join(codexHome, "auth.json")
	fi, err := os.Lstat(authPath)
	if err != nil {
		t.Fatalf("auth.json not found: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("auth.json should be a symlink")
	}
	target, _ := os.Readlink(authPath)
	if target != filepath.Join(sharedHome, "auth.json") {
		t.Errorf("auth.json symlink target = %q, want %q", target, filepath.Join(sharedHome, "auth.json"))
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

	// config.toml should be copied.
	data, _ = os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if string(data) != `model = "o3"` {
		t.Errorf("config.toml content = %q", data)
	}

	// instructions.md should be copied.
	data, _ = os.ReadFile(filepath.Join(codexHome, "instructions.md"))
	if string(data) != "Be helpful." {
		t.Errorf("instructions.md content = %q", data)
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

	// Directory should exist but be empty (no auth.json, no config.json, etc.).
	entries, err := os.ReadDir(codexHome)
	if err != nil {
		t.Fatalf("failed to read codex-home: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("expected empty codex-home, got: %v", names)
	}
}

func TestEnsureSymlinkRepairsBrokenLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "source.json")
	dst := filepath.Join(dir, "link.json")

	os.WriteFile(src, []byte("real"), 0o644)

	// Create a broken symlink pointing to a non-existent file.
	os.Symlink(filepath.Join(dir, "old-source.json"), dst)

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
