package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeTestLocalSkill(t *testing.T, root, rel string, files map[string]string) string {
	t.Helper()

	skillDir := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	for path, content := range files {
		fullPath := filepath.Join(skillDir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir parents for %s: %v", path, err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	return skillDir
}

func TestListRuntimeLocalSkills_Claude(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".claude", "skills"), "review-helper", map[string]string{
		"SKILL.md":           "---\nname: Review Helper\ndescription: Review pull requests\n---\n# Review Helper\n",
		"templates/check.md": "checklist",
		"LICENSE":            "ignored",
		".secret":            "ignored",
	})

	skills, supported, err := listRuntimeLocalSkills("claude")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}

	skill := skills[0]
	if skill.Key != "review-helper" {
		t.Fatalf("key = %q, want review-helper", skill.Key)
	}
	if skill.Name != "Review Helper" {
		t.Fatalf("name = %q, want Review Helper", skill.Name)
	}
	if skill.Description != "Review pull requests" {
		t.Fatalf("description = %q", skill.Description)
	}
	// 2 = supporting file (templates/check.md) + SKILL.md itself.
	// Bundle file count purposely excludes SKILL.md (it travels in
	// `Content`) but the summary count adds it back so the user sees
	// the real total.
	if skill.FileCount != 2 {
		t.Fatalf("file_count = %d, want 2", skill.FileCount)
	}
	if skill.SourcePath != "~/.claude/skills/review-helper" {
		t.Fatalf("source_path = %q", skill.SourcePath)
	}
}

func TestListRuntimeLocalSkills_Kiro(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".kiro", "skills"), "review-helper", map[string]string{
		"SKILL.md": "---\nname: Kiro Review\ndescription: Review code with Kiro\n---\n# Kiro Review\n",
	})

	skills, supported, err := listRuntimeLocalSkills("kiro")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("kiro should be supported")
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Key != "review-helper" {
		t.Fatalf("key = %q, want review-helper", skills[0].Key)
	}
	if skills[0].Name != "Kiro Review" {
		t.Fatalf("name = %q, want Kiro Review", skills[0].Name)
	}
	if skills[0].SourcePath != "~/.kiro/skills/review-helper" {
		t.Fatalf("source_path = %q", skills[0].SourcePath)
	}
}

// Skill installers (for example lark-cli) place every skill at a shared
// location like ~/.agents/skills/<name> and symlink each one into the
// runtime root (~/.claude/skills/<name>). The previous filepath.WalkDir
// path filtered every symlink out via os.ModeSymlink, so users with
// dozens of installed skills only saw the few they had cloned in place.
// listRuntimeLocalSkills must follow those symlinks.
func TestListRuntimeLocalSkills_FollowsSymlinkedSkillDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Real skill lives outside the runtime root.
	target := writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "lark-doc", map[string]string{
		"SKILL.md":  "---\nname: Lark Doc\ndescription: Drive lark docs\n---\n# Lark Doc\n",
		"helper.md": "stub",
	})

	// Runtime root points at it via symlink, the way installers ship it.
	skillsRoot := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		t.Fatalf("mkdir skills root: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(skillsRoot, "lark-doc")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// Sanity: also seed a regular non-symlink skill so we know enumeration
	// returns both, in stable order.
	writeTestLocalSkill(t, skillsRoot, "review-helper", map[string]string{
		"SKILL.md": "---\nname: Review Helper\n---\n",
	})

	skills, supported, err := listRuntimeLocalSkills("claude")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d (%v)", len(skills), skills)
	}

	bySymlinkKey := skills[0]
	if bySymlinkKey.Key != "lark-doc" {
		bySymlinkKey = skills[1]
	}
	if bySymlinkKey.Key != "lark-doc" {
		t.Fatalf("symlinked skill missing from result: %v", skills)
	}
	if bySymlinkKey.Name != "Lark Doc" {
		t.Fatalf("symlinked skill name = %q, want Lark Doc", bySymlinkKey.Name)
	}
	// Source path is reported relative to the *runtime root* (~/.claude/...),
	// not the resolved target — that's what the user expects to see in the
	// import dialog and matches the non-symlink case.
	if bySymlinkKey.SourcePath != "~/.claude/skills/lark-doc" {
		t.Fatalf("symlinked skill source_path = %q", bySymlinkKey.SourcePath)
	}
}

func TestListRuntimeLocalSkills_CodexUsesSharedCODEXHOME(t *testing.T) {
	home := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", codexHome)

	writeTestLocalSkill(t, filepath.Join(codexHome, "skills"), "debugger", map[string]string{
		"SKILL.md": "# Debugger\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".codex", "skills"), "wrong-home", map[string]string{
		"SKILL.md": "# Wrong Home\n",
	})

	skills, supported, err := listRuntimeLocalSkills("codex")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("codex should be supported")
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Key != "debugger" {
		t.Fatalf("key = %q, want debugger", skills[0].Key)
	}
	if skills[0].SourcePath != filepath.Join(codexHome, "skills", "debugger") {
		t.Fatalf("source_path = %q", skills[0].SourcePath)
	}
}

func TestListRuntimeLocalSkills_CopilotUsesConfiguredAndSharedRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".copilot", "skills"), "review-helper", map[string]string{
		"SKILL.md": "---\nname: Copilot Review\n---\n# Copilot Review\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "lark-doc", map[string]string{
		"SKILL.md": "---\nname: Lark Doc\n---\n# Lark Doc\n",
	})

	skills, supported, err := listRuntimeLocalSkills("copilot")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("copilot should be supported")
	}

	keys := make([]string, 0, len(skills))
	for _, s := range skills {
		keys = append(keys, s.Key)
	}
	wantKeys := []string{"lark-doc", "review-helper"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("keys = %v, want %v", keys, wantKeys)
	}

	bundle, supported, err := loadRuntimeLocalSkillBundle("copilot", "lark-doc")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported {
		t.Fatal("copilot should be supported")
	}
	if bundle.SourcePath != "~/.agents/skills/lark-doc" {
		t.Fatalf("source_path = %q", bundle.SourcePath)
	}
}

func TestLoadRuntimeLocalSkillBundle_SkipsInvalidEarlierRootCandidate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := os.MkdirAll(filepath.Join(home, ".copilot", "skills", "lark-doc"), 0o755); err != nil {
		t.Fatalf("mkdir invalid candidate: %v", err)
	}
	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "lark-doc", map[string]string{
		"SKILL.md": "---\nname: Lark Doc\n---\n# Lark Doc\n",
	})

	skills, supported, err := listRuntimeLocalSkills("copilot")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("copilot should be supported")
	}
	if len(skills) != 1 || skills[0].SourcePath != "~/.agents/skills/lark-doc" {
		t.Fatalf("skills = %+v, want shared lark-doc only", skills)
	}

	bundle, supported, err := loadRuntimeLocalSkillBundle("copilot", "lark-doc")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported {
		t.Fatal("copilot should be supported")
	}
	if bundle.Name != "Lark Doc" {
		t.Fatalf("name = %q, want Lark Doc", bundle.Name)
	}
	if bundle.SourcePath != "~/.agents/skills/lark-doc" {
		t.Fatalf("source_path = %q", bundle.SourcePath)
	}
}

// opencode (and possibly future providers) lay skills out one level deep,
// e.g. ~/.config/opencode/skills/release/reporter/SKILL.md.
// loadRuntimeLocalSkillBundle already accepts that nested key, so the list
// endpoint must surface those skills too — otherwise the import dialog
// hides skills the load endpoint can fetch and users can't pick them.
//
// The walker also has to short-circuit at the outermost SKILL.md it finds:
// nested SKILL.md files inside an already-registered skill (e.g. inside
// `top/SKILL.md`'s own template tree) are part of the parent skill's
// bundle, not separate skills.
func TestListRuntimeLocalSkills_DescendsIntoNestedSkillDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	root := filepath.Join(home, ".config", "opencode", "skills")

	// Top-level skill — should register at key="top" and its child SKILL.md
	// must NOT register as a separate skill.
	writeTestLocalSkill(t, root, "top", map[string]string{
		"SKILL.md":           "---\nname: Top\n---\n",
		"templates/SKILL.md": "not a real skill — sub-template that happens to share the filename",
	})

	// Nested skill — only valid SKILL.md is at depth 2.
	writeTestLocalSkill(t, root, "release/reporter", map[string]string{
		"SKILL.md": "---\nname: Release Reporter\n---\n",
	})

	skills, supported, err := listRuntimeLocalSkills("opencode")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("opencode should be supported")
	}

	keys := make([]string, 0, len(skills))
	for _, s := range skills {
		keys = append(keys, s.Key)
	}
	// Two registered skills, "top" and "release/reporter" — and crucially
	// NOT "top/templates" (the inner SKILL.md must be ignored once the
	// parent qualified).
	wantKeys := []string{"release/reporter", "top"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("keys = %v, want %v", keys, wantKeys)
	}
}

func TestListRuntimeLocalSkills_OpenCodeUsesLegacyAndSharedRoots(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".opencode", "skills"), "native-helper", map[string]string{
		"SKILL.md": "---\nname: Native Helper\n---\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".config", "opencode", "skills"), "release/reporter", map[string]string{
		"SKILL.md": "---\nname: Release Reporter\n---\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".claude", "skills"), "review-helper", map[string]string{
		"SKILL.md": "---\nname: Review Helper\n---\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".agents", "skills"), "lark-doc", map[string]string{
		"SKILL.md": "---\nname: Lark Doc\n---\n",
	})

	skills, supported, err := listRuntimeLocalSkills("opencode")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("opencode should be supported")
	}

	keys := make([]string, 0, len(skills))
	for _, s := range skills {
		keys = append(keys, s.Key)
	}
	wantKeys := []string{"lark-doc", "native-helper", "release/reporter", "review-helper"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("keys = %v, want %v", keys, wantKeys)
	}

	bundle, supported, err := loadRuntimeLocalSkillBundle("opencode", "review-helper")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported {
		t.Fatal("opencode should be supported")
	}
	if bundle.SourcePath != "~/.claude/skills/review-helper" {
		t.Fatalf("source_path = %q", bundle.SourcePath)
	}
}

func TestLoadRuntimeLocalSkillBundle_OpenCode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".config", "opencode", "skills"), "release/reporter", map[string]string{
		"SKILL.md":           "---\nname: Release Reporter\ndescription: Summarize release notes\n---\n# Release Reporter\n",
		"docs/template.md":   "template body",
		"examples/sample.md": "sample body",
	})

	bundle, supported, err := loadRuntimeLocalSkillBundle("opencode", "release/reporter")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported {
		t.Fatal("opencode should be supported")
	}
	if bundle.Name != "Release Reporter" {
		t.Fatalf("name = %q", bundle.Name)
	}
	if bundle.Description != "Summarize release notes" {
		t.Fatalf("description = %q", bundle.Description)
	}
	if len(bundle.Files) != 2 {
		t.Fatalf("expected 2 supporting files, got %d", len(bundle.Files))
	}
	if bundle.Files[0].Path != "docs/template.md" || bundle.Files[0].Content != "template body" {
		t.Fatalf("unexpected first file: %+v", bundle.Files[0])
	}
	if bundle.Files[1].Path != "examples/sample.md" || bundle.Files[1].Content != "sample body" {
		t.Fatalf("unexpected second file: %+v", bundle.Files[1])
	}
	if bundle.SourcePath != "~/.config/opencode/skills/release/reporter" {
		t.Fatalf("source_path = %q", bundle.SourcePath)
	}
}

func TestListRuntimeLocalSkills_OpenClaw(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".openclaw", "skills"), "planner", map[string]string{
		"SKILL.md": "# Planner\n",
	})

	skills, supported, err := listRuntimeLocalSkills("openclaw")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("openclaw should be supported")
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].SourcePath != "~/.openclaw/skills/planner" {
		t.Fatalf("source_path = %q", skills[0].SourcePath)
	}
}

func TestLoadRuntimeLocalSkillBundle_OpenClawPrefersWorkspaceSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".openclaw", "skills"), "planner", map[string]string{
		"SKILL.md": "---\nname: Global Planner\n---\n# Global Planner\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".openclaw", "workspace", "skills"), "planner", map[string]string{
		"SKILL.md":             "---\nname: Workspace Planner\n---\n# Workspace Planner\n",
		"references/guide.md":  "guide",
		"scripts/bootstrap.sh": "bootstrap",
		"LICENSE":              "ignored",
		"scripts/__pycache__/bootstrap.cpython-312.pyc": "compiled",
		"node_modules/pkg/index.js":                     "dependency",
		"assets/logo.png":                               "png",
	})

	skills, supported, err := listRuntimeLocalSkills("openclaw")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("openclaw should be supported")
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d (%v)", len(skills), skills)
	}
	if skills[0].Name != "Workspace Planner" {
		t.Fatalf("name = %q, want Workspace Planner", skills[0].Name)
	}
	if skills[0].SourcePath != "~/.openclaw/workspace/skills/planner" {
		t.Fatalf("source_path = %q", skills[0].SourcePath)
	}
	if skills[0].FileCount != 3 {
		t.Fatalf("file_count = %d, want 3", skills[0].FileCount)
	}

	bundle, supported, err := loadRuntimeLocalSkillBundle("openclaw", "planner")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported {
		t.Fatal("openclaw should be supported")
	}
	if bundle.Name != "Workspace Planner" {
		t.Fatalf("bundle name = %q, want Workspace Planner", bundle.Name)
	}
	if bundle.SourcePath != "~/.openclaw/workspace/skills/planner" {
		t.Fatalf("bundle source_path = %q", bundle.SourcePath)
	}
	if len(bundle.Files) != 2 {
		t.Fatalf("expected 2 supporting files, got %d", len(bundle.Files))
	}
	if bundle.Files[0].Path != "references/guide.md" || bundle.Files[0].Content != "guide" {
		t.Fatalf("unexpected first file: %+v", bundle.Files[0])
	}
	if bundle.Files[1].Path != "scripts/bootstrap.sh" || bundle.Files[1].Content != "bootstrap" {
		t.Fatalf("unexpected second file: %+v", bundle.Files[1])
	}
}

func TestListRuntimeLocalSkills_WujieClaw(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".openclaw", "skills"), "legacy", map[string]string{
		"SKILL.md": "# Legacy\n",
	})
	writeTestLocalSkill(t, filepath.Join(home, ".wujieclaw", "workspace", "skills"), "planner", map[string]string{
		"SKILL.md": "# Planner\n",
	})

	skills, supported, err := listRuntimeLocalSkills("wujieclaw")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("wujieclaw should be supported")
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}
	sourcePaths := make(map[string]bool, len(skills))
	for _, skill := range skills {
		sourcePaths[skill.SourcePath] = true
	}
	for _, want := range []string{
		"~/.wujieclaw/workspace/skills/planner",
		"~/.openclaw/skills/legacy",
	} {
		if !sourcePaths[want] {
			t.Fatalf("missing source_path %q in %+v", want, skills)
		}
	}
}

func TestLoadRuntimeLocalSkillBundle_Cursor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	writeTestLocalSkill(t, filepath.Join(home, ".cursor", "skills"), "docs-helper", map[string]string{
		"SKILL.md":         "---\nname: Docs Helper\n---\n# Docs Helper\n",
		"notes/tips.md":    "tips",
		"examples/a.txt":   "example",
		".hidden/skip.txt": "ignore",
	})

	bundle, supported, err := loadRuntimeLocalSkillBundle("cursor", "docs-helper")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported {
		t.Fatal("cursor should be supported")
	}
	if bundle.Name != "Docs Helper" {
		t.Fatalf("name = %q", bundle.Name)
	}
	if len(bundle.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(bundle.Files))
	}
	if bundle.SourcePath != "~/.cursor/skills/docs-helper" {
		t.Fatalf("source_path = %q", bundle.SourcePath)
	}
}

func writeTestClaudeCommand(t *testing.T, root, rel, content string) {
	t.Helper()
	fullPath := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatalf("mkdir for command: %v", err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write command: %v", err)
	}
}

func TestListRuntimeLocalSkills_ClaudeCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a regular skill AND a command.
	writeTestLocalSkill(t, filepath.Join(home, ".claude", "skills"), "review-helper", map[string]string{
		"SKILL.md": "---\nname: Review Helper\ndescription: Review pull requests\n---\n",
	})
	cmdRoot := filepath.Join(home, ".claude", "commands")
	writeTestClaudeCommand(t, cmdRoot, "summarize.md", "---\nname: Summarize\ndescription: Summarize text\n---\n# Summarize\n")

	skills, supported, err := listRuntimeLocalSkills("claude")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}

	byKey := make(map[string]runtimeLocalSkillSummary)
	for _, s := range skills {
		byKey[s.Key] = s
	}

	if _, ok := byKey["review-helper"]; !ok {
		t.Fatal("expected skill key 'review-helper'")
	}
	cmd, ok := byKey["commands/summarize.md"]
	if !ok {
		t.Fatalf("expected command key 'commands/summarize.md', got keys: %v", keysOf(byKey))
	}
	if cmd.Name != "Summarize" {
		t.Fatalf("command name = %q, want Summarize", cmd.Name)
	}
	if cmd.Description != "Summarize text" {
		t.Fatalf("command description = %q", cmd.Description)
	}
	if cmd.FileCount != 1 {
		t.Fatalf("command file_count = %d, want 1", cmd.FileCount)
	}
	if cmd.SourcePath != "~/.claude/commands/summarize.md" {
		t.Fatalf("command source_path = %q", cmd.SourcePath)
	}
}

func TestListRuntimeLocalSkills_ClaudeNestedCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmdRoot := filepath.Join(home, ".claude", "commands")
	writeTestClaudeCommand(t, cmdRoot, "top-level.md", "# Top\n")
	writeTestClaudeCommand(t, cmdRoot, "code/review.md", "---\nname: Code Review\n---\n")
	writeTestClaudeCommand(t, cmdRoot, "code/refactor.md", "# Refactor\n")
	// Hidden and non-md files should be ignored.
	writeTestClaudeCommand(t, cmdRoot, ".hidden.md", "# Hidden\n")
	writeTestClaudeCommand(t, cmdRoot, "notes.txt", "not markdown")

	// skills dir doesn't exist — only commands should show up.
	skills, supported, err := listRuntimeLocalSkills("claude")
	if err != nil {
		t.Fatalf("listRuntimeLocalSkills: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}

	keys := make([]string, 0, len(skills))
	for _, s := range skills {
		keys = append(keys, s.Key)
	}
	wantKeys := []string{"commands/code/refactor.md", "commands/code/review.md", "commands/top-level.md"}
	if !reflect.DeepEqual(keys, wantKeys) {
		t.Fatalf("keys = %v, want %v", keys, wantKeys)
	}

	// Verify name fallback: top-level.md has no frontmatter name → filename.
	topKey := skills[0]
	if topKey.Key == "commands/top-level.md" {
		if topKey.Name != "top-level" {
			t.Fatalf("fallback name = %q, want top-level", topKey.Name)
		}
	}

	// Verify nested frontmatter name.
	for _, s := range skills {
		if s.Key == "commands/code/review.md" && s.Name != "Code Review" {
			t.Fatalf("nested command name = %q, want Code Review", s.Name)
		}
	}
}

func TestLoadRuntimeLocalSkillBundle_ClaudeCommand(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmdRoot := filepath.Join(home, ".claude", "commands")
	writeTestClaudeCommand(t, cmdRoot, "my-cmd.md", "---\nname: My Command\ndescription: Does stuff\n---\n# My Command\nBody here.\n")

	bundle, supported, err := loadRuntimeLocalSkillBundle("claude", "commands/my-cmd.md")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}
	if bundle.Name != "My Command" {
		t.Fatalf("name = %q", bundle.Name)
	}
	if bundle.Description != "Does stuff" {
		t.Fatalf("description = %q", bundle.Description)
	}
	if bundle.Content != "---\nname: My Command\ndescription: Does stuff\n---\n# My Command\nBody here.\n" {
		t.Fatalf("content = %q", bundle.Content)
	}
	if bundle.SourcePath != "~/.claude/commands/my-cmd.md" {
		t.Fatalf("source_path = %q", bundle.SourcePath)
	}
	if bundle.Provider != "claude" {
		t.Fatalf("provider = %q", bundle.Provider)
	}
	if len(bundle.Files) != 0 {
		t.Fatalf("expected no files, got %d", len(bundle.Files))
	}
}

func TestLoadRuntimeLocalSkillBundle_ClaudeCommandNoFrontmatter(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmdRoot := filepath.Join(home, ".claude", "commands")
	writeTestClaudeCommand(t, cmdRoot, "simple.md", "# Simple command\n")

	bundle, supported, err := loadRuntimeLocalSkillBundle("claude", "commands/simple.md")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}
	if bundle.Name != "simple" {
		t.Fatalf("name = %q, want simple", bundle.Name)
	}
	if bundle.Description != "" {
		t.Fatalf("expected empty description, got %q", bundle.Description)
	}
}

func TestLoadRuntimeLocalSkillBundle_ClaudeCommandNested(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cmdRoot := filepath.Join(home, ".claude", "commands")
	writeTestClaudeCommand(t, cmdRoot, "code/review.md", "---\nname: Review\n---\n# Review\n")

	bundle, supported, err := loadRuntimeLocalSkillBundle("claude", "commands/code/review.md")
	if err != nil {
		t.Fatalf("loadRuntimeLocalSkillBundle: %v", err)
	}
	if !supported {
		t.Fatal("claude should be supported")
	}
	if bundle.Name != "Review" {
		t.Fatalf("name = %q", bundle.Name)
	}
	if bundle.SourcePath != "~/.claude/commands/code/review.md" {
		t.Fatalf("source_path = %q", bundle.SourcePath)
	}
}

func TestLoadRuntimeLocalSkillBundle_ClaudeCommandNotFound(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	_, supported, err := loadRuntimeLocalSkillBundle("claude", "commands/nonexistent.md")
	if !supported {
		t.Fatal("claude should be supported")
	}
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func keysOf(m map[string]runtimeLocalSkillSummary) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
