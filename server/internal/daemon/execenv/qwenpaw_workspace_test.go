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

func TestPrepareQwenpawWorkspacePreservesCollidingSkillSlugs(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	skills := []SkillContextForEnv{
		{Name: "A B", Content: "# First\n\nfirst skill body"},
		{Name: "A-B", Content: "# Second\n\nsecond skill body"},
	}

	if err := prepareQwenpawWorkspace(workspaceDir, skills, testLogger()); err != nil {
		t.Fatalf("prepareQwenpawWorkspace failed: %v", err)
	}

	expectedSkills := map[string]string{
		"a-b":         "first skill body",
		"a-b-multica": "second skill body",
	}
	for slug, wantBody := range expectedSkills {
		data, err := os.ReadFile(filepath.Join(workspaceDir, "skills", slug, "SKILL.md"))
		if err != nil {
			t.Fatalf("read SKILL.md for %q: %v", slug, err)
		}
		body := string(data)
		if !frontmatterNameIs(body, slug) {
			t.Errorf("SKILL.md for %q does not use its directory slug in frontmatter", slug)
		}
		if !strings.Contains(body, wantBody) {
			t.Errorf("SKILL.md for %q does not contain %q", slug, wantBody)
		}
	}

	data, err := os.ReadFile(filepath.Join(workspaceDir, "skill.json"))
	if err != nil {
		t.Fatalf("read skill.json: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("unmarshal skill.json: %v", err)
	}
	skillsMap, ok := manifest["skills"].(map[string]any)
	if !ok {
		t.Fatal("skills is not a map")
	}
	if len(skillsMap) != len(expectedSkills) {
		t.Fatalf("manifest has %d skills, want %d", len(skillsMap), len(expectedSkills))
	}
	for slug := range expectedSkills {
		entry, ok := skillsMap[slug].(map[string]any)
		if !ok {
			t.Fatalf("skill entry %q is not a map", slug)
		}
		metadata, ok := entry["metadata"].(map[string]any)
		if !ok {
			t.Fatalf("metadata for %q is not a map", slug)
		}
		if metadata["name"] != slug {
			t.Errorf("metadata name for %q = %v, want %q", slug, metadata["name"], slug)
		}
	}
}

// TestPrepareQwenpawWorkspaceEmpty verifies that an empty skills list creates
// the workspace directory but removes any existing skills dir and manifest.
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

// TestPrepareQwenpawWorkspaceRevokeAll verifies that unbinding every skill
// (A → ∅) properly removes the skills dir and manifest so the agent
// no longer has access to previously bound skills.
func TestPrepareQwenpawWorkspaceRevokeAll(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()

	// Step 1: prepare with skills (A)
	skills := []SkillContextForEnv{
		{Name: "Deploy Helper", Content: "# Deploy Helper\n\nHelps deploy."},
	}
	if err := prepareQwenpawWorkspace(workspaceDir, skills, testLogger()); err != nil {
		t.Fatalf("first prepare failed: %v", err)
	}

	// Verify skills and manifest exist
	if _, err := os.Stat(filepath.Join(workspaceDir, "skills")); err != nil {
		t.Fatalf("skills should exist after first prepare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspaceDir, "skill.json")); err != nil {
		t.Fatalf("skill.json should exist after first prepare: %v", err)
	}

	// Step 2: prepare with no skills (A → ∅)
	if err := prepareQwenpawWorkspace(workspaceDir, nil, testLogger()); err != nil {
		t.Fatalf("revoke prepare failed: %v", err)
	}

	// Verify skills and manifest are gone
	if _, err := os.Stat(filepath.Join(workspaceDir, "skills")); !os.IsNotExist(err) {
		t.Error("skills should be removed after revoking all skills")
	}
	if _, err := os.Stat(filepath.Join(workspaceDir, "skill.json")); !os.IsNotExist(err) {
		t.Error("skill.json should be removed after revoking all skills")
	}
}

// TestPrepareQwenpawWorkspaceReplace verifies that replacing skills (A → B)
// correctly removes old skills and writes new ones without collision suffixes.
func TestPrepareQwenpawWorkspaceReplace(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()

	// Step 1: prepare with skill A
	skillsA := []SkillContextForEnv{
		{Name: "Deploy Helper", Content: "# Deploy Helper\n\nHelps deploy."},
	}
	if err := prepareQwenpawWorkspace(workspaceDir, skillsA, testLogger()); err != nil {
		t.Fatalf("first prepare (A) failed: %v", err)
	}

	// Verify deploy-helper exists
	skillsDir := filepath.Join(workspaceDir, "skills")
	if _, err := os.Stat(filepath.Join(skillsDir, "deploy-helper")); err != nil {
		t.Fatalf("deploy-helper dir should exist: %v", err)
	}

	// Step 2: prepare with skill B (different set)
	skillsB := []SkillContextForEnv{
		{Name: "Bug Finder", Content: "# Bug Finder\n\nFinds bugs."},
		{Name: "Review Helper", Content: "# Review Helper\n\nReviews code."},
	}
	if err := prepareQwenpawWorkspace(workspaceDir, skillsB, testLogger()); err != nil {
		t.Fatalf("second prepare (B) failed: %v", err)
	}

	// Verify old skill is gone
	if _, err := os.Stat(filepath.Join(skillsDir, "deploy-helper")); !os.IsNotExist(err) {
		t.Error("deploy-helper should be removed after replacing skills")
	}

	// Verify new skills exist with natural slugs (no collision suffix)
	for _, slug := range []string{"bug-finder", "review-helper"} {
		dir := filepath.Join(skillsDir, slug)
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("new skill dir %q should exist: %v", slug, err)
		}
		// Verify no collision suffix
		if _, err := os.Stat(filepath.Join(skillsDir, slug+"-multica")); !os.IsNotExist(err) {
			t.Errorf("slug %q should not have a collision suffix after replace", slug)
		}
	}

	// Verify manifest has exactly 2 skills
	manifestPath := filepath.Join(workspaceDir, "skill.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read skill.json: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("unmarshal skill.json: %v", err)
	}
	skillsMap, ok := manifest["skills"].(map[string]any)
	if !ok {
		t.Fatal("skills is not a map")
	}
	if len(skillsMap) != 2 {
		t.Errorf("expected 2 skills in manifest after replace, got %d", len(skillsMap))
	}
}

// TestPrepareQwenpawWorkspaceRepeatedReuse verifies that preparing the same
// workspace with the same skill set multiple times (A → A) is idempotent —
// no collision suffixes, no duplicate entries, manifest unchanged.
func TestPrepareQwenpawWorkspaceRepeatedReuse(t *testing.T) {
	t.Parallel()

	workspaceDir := t.TempDir()
	skills := []SkillContextForEnv{
		{Name: "Deploy Helper", Content: "# Deploy Helper\n\nHelps deploy."},
		{Name: "Bug Finder", Content: "# Bug Finder\n\nFinds bugs."},
	}

	// Prepare three times with the same set
	for i := 0; i < 3; i++ {
		if err := prepareQwenpawWorkspace(workspaceDir, skills, testLogger()); err != nil {
			t.Fatalf("prepare iteration %d failed: %v", i, err)
		}
	}

	skillsDir := filepath.Join(workspaceDir, "skills")

	// Verify each skill exists exactly once (no collision suffixes)
	for _, slug := range []string{"deploy-helper", "bug-finder"} {
		dir := filepath.Join(skillsDir, slug)
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("skill dir %q should exist: %v", slug, err)
		}
		// No collision suffix
		if _, err := os.Stat(filepath.Join(skillsDir, slug+"-multica")); !os.IsNotExist(err) {
			t.Errorf("slug %q should not have a collision suffix after repeated reuse", slug)
		}
		if _, err := os.Stat(filepath.Join(skillsDir, slug+"-multica-2")); !os.IsNotExist(err) {
			t.Errorf("slug %q should not have a collision suffix after repeated reuse", slug)
		}
	}

	// Verify only the two expected dirs exist in skills
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read skills dir: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries in skills, got %d", len(entries))
	}

	// Verify manifest has exactly 2 skills
	manifestPath := filepath.Join(workspaceDir, "skill.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read skill.json: %v", err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("unmarshal skill.json: %v", err)
	}
	skillsMap, ok := manifest["skills"].(map[string]any)
	if !ok {
		t.Fatal("skills is not a map")
	}
	if len(skillsMap) != 2 {
		t.Errorf("expected 2 skills in manifest after repeated reuse, got %d", len(skillsMap))
	}
}
