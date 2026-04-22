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
		"SKILL.md":            "---\nname: Top\n---\n",
		"templates/SKILL.md":  "not a real skill — sub-template that happens to share the filename",
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
