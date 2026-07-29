package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPrepareQwenpawWorkspace verifies that the workspace is created with the
// correct skills directory structure and skill.json manifest with enabled: true.
func TestPrepareQwenpawWorkspace(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	skills := []SkillContextForEnv{
		{
			Name:        "Review Helper",
			Description: "Helps review code changes",
			Content:     "# Review Helper\n\nThis skill helps review code.",
		},
		{
			Name:        "Bug Finder",
			Description: "Finds bugs in code",
			Content:     "# Bug Finder\n\nThis skill finds bugs.",
		},
	}

	if err := prepareQwenpawWorkspace(workspaceDir, skills, testLogger()); err != nil {
		t.Fatalf("prepareQwenpawWorkspace failed: %v", err)
	}

	// Check that skills dir exists
	skillsDir := filepath.Join(workspaceDir, "skills")
	if fi, err := os.Stat(skillsDir); err != nil {
		t.Fatalf("skills dir not created: %v", err)
	} else if !fi.IsDir() {
		t.Fatal("skills dir is not a directory")
	}

	// Check that each skill has SKILL.md
	for _, slug := range []string{"review-helper", "bug-finder"} {
		skillDir := filepath.Join(skillsDir, slug)
		if fi, err := os.Stat(skillDir); err != nil {
			t.Fatalf("skill dir %q not created: %v", slug, err)
		} else if !fi.IsDir() {
			t.Fatalf("skill dir %q is not a directory", slug)
		}
		body, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
		if err != nil {
			t.Fatalf("read SKILL.md for %q: %v", slug, err)
		}
		if !strings.Contains(string(body), slug) {
			t.Errorf("SKILL.md for %q should contain its slug in frontmatter", slug)
		}
	}

	// Check that skill.json manifest exists with enabled: true
	manifestPath := filepath.Join(workspaceDir, "skill.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read skill.json: %v", err)
	}

	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("unmarshal skill.json: %v", err)
	}

	if manifest["schema_version"] != "workspace-skill-manifest.v1" {
		t.Errorf("schema_version = %q, want workspace-skill-manifest.v1", manifest["schema_version"])
	}

	skillsMap, ok := manifest["skills"].(map[string]any)
	if !ok {
		t.Fatal("skills is not a map")
	}
	if len(skillsMap) != 2 {
		t.Fatalf("expected 2 skills in manifest, got %d", len(skillsMap))
	}

	for _, slug := range []string{"review-helper", "bug-finder"} {
		entry, ok := skillsMap[slug].(map[string]any)
		if !ok {
			t.Fatalf("skill entry %q is not a map", slug)
		}
		enabled, ok := entry["enabled"].(bool)
		if !ok || !enabled {
			t.Errorf("skill %q enabled = %v, want true", slug, enabled)
		}
		channels, ok := entry["channels"].([]any)
		if !ok || len(channels) != 1 || channels[0] != "all" {
			t.Errorf("skill %q channels = %v, want [\"all\"]", slug, channels)
		}
	}
}

// TestPrepareQwenpawWorkspaceEmpty verifies that an empty skills list creates
// the workspace directory but no skills dir or manifest.
func TestPrepareQwenpawWorkspaceEmpty(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	if err := prepareQwenpawWorkspace(workspaceDir, nil, testLogger()); err != nil {
		t.Fatalf("prepareQwenpawWorkspace failed: %v", err)
	}

	// Workspace dir should exist
	if fi, err := os.Stat(workspaceDir); err != nil {
		t.Fatalf("workspace dir not created: %v", err)
	} else if !fi.IsDir() {
		t.Fatal("workspace dir is not a directory")
	}

	// No skills dir should exist
	if _, err := os.Stat(filepath.Join(workspaceDir, "skills")); !os.IsNotExist(err) {
		t.Error("skills dir should not be created for empty skills list")
	}

	// No skill.json should exist
	if _, err := os.Stat(filepath.Join(workspaceDir, "skill.json")); !os.IsNotExist(err) {
		t.Error("skill.json should not be created for empty skills list")
	}
}

// TestPrepareQwenpawWorkspaceWithFiles verifies that skill supporting files
// are written correctly.
func TestPrepareQwenpawWorkspaceWithFiles(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	skills := []SkillContextForEnv{
		{
			Name:        "Data Analyzer",
			Description: "Analyzes data files",
			Content:     "# Data Analyzer\n\nAnalyzes data.",
			Files: []SkillFileContextForEnv{
				{Path: "scripts/analyze.py", Content: "print('analyzing')"},
				{Path: "config.json", Content: `{"key": "value"}`},
			},
		},
	}

	if err := prepareQwenpawWorkspace(workspaceDir, skills, testLogger()); err != nil {
		t.Fatalf("prepareQwenpawWorkspace failed: %v", err)
	}

	skillDir := filepath.Join(workspaceDir, "skills", "data-analyzer")

	// Check SKILL.md
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md not found: %v", err)
	}

	// Check supporting files
	if _, err := os.Stat(filepath.Join(skillDir, "scripts", "analyze.py")); err != nil {
		t.Fatalf("scripts/analyze.py not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "config.json")); err != nil {
		t.Fatalf("config.json not found: %v", err)
	}

	// Verify content
	body, err := os.ReadFile(filepath.Join(skillDir, "scripts", "analyze.py"))
	if err != nil {
		t.Fatalf("read scripts/analyze.py: %v", err)
	}
	if string(body) != "print('analyzing')" {
		t.Errorf("scripts/analyze.py content = %q, want %q", string(body), "print('analyzing')")
	}
}

// TestPrepareQwenpawWorkspacePermissions verifies workspace directory
// permissions are set correctly.
func TestPrepareQwenpawWorkspacePermissions(t *testing.T) {
	t.Parallel()

	workspaceDir := filepath.Join(t.TempDir(), "qwenpaw-workspace")
	skills := []SkillContextForEnv{
		{Name: "Test Skill", Content: "# Test Skill\n\nTest."},
	}

	if err := prepareQwenpawWorkspace(workspaceDir, skills, testLogger()); err != nil {
		t.Fatalf("prepareQwenpawWorkspace failed: %v", err)
	}

	if fi, err := os.Stat(workspaceDir); err != nil {
		t.Fatalf("stat workspace: %v", err)
	} else if fi.Mode().Perm() != 0o700 {
		t.Errorf("workspace perms = %o, want 0700", fi.Mode().Perm())
	}
}