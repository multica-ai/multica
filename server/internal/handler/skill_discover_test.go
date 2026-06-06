package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSelectSkillDirsFromTree(t *testing.T) {
	tree := []githubTreeEntry{
		{Path: "skills/a/SKILL.md", Type: "blob"},
		{Path: "skills/a/ref.md", Type: "blob"},
		{Path: "skills/b/SKILL.md", Type: "blob"},
		{Path: "README.md", Type: "blob"},
		{Path: "skills/a", Type: "tree"},
	}
	dirs := selectSkillDirsFromTree(tree, "")
	if len(dirs) != 2 {
		t.Fatalf("want 2 skill dirs, got %d: %v", len(dirs), dirs)
	}
	if dirs[0] != "skills/a" || dirs[1] != "skills/b" {
		t.Fatalf("unexpected dirs: %v", dirs)
	}
}

func TestSelectSkillDirsScopedToSubdir(t *testing.T) {
	tree := []githubTreeEntry{
		{Path: "skills/a/SKILL.md", Type: "blob"},
		{Path: "other/c/SKILL.md", Type: "blob"},
	}
	dirs := selectSkillDirsFromTree(tree, "skills")
	if len(dirs) != 1 || dirs[0] != "skills/a" {
		t.Fatalf("scoping failed: %v", dirs)
	}
}

func TestSelectSkillDirsRootSkill(t *testing.T) {
	tree := []githubTreeEntry{{Path: "SKILL.md", Type: "blob"}}
	dirs := selectSkillDirsFromTree(tree, "")
	if len(dirs) != 1 || dirs[0] != "" {
		t.Fatalf("root skill not detected: %v", dirs)
	}
}

func TestDiscoverSkillsRejectsNonGitHub(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/skills/discover",
		strings.NewReader(`{"url":"https://clawhub.ai/owner/skill"}`))
	req.Header.Set("Content-Type", "application/json")
	testHandler.DiscoverSkills(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-GitHub url, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDiscoverSkillsRejectsInvalidBody(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/skills/discover", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	testHandler.DiscoverSkills(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad body, got %d", w.Code)
	}
}
