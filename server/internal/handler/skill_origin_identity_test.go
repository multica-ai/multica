package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// installGitHubFixtureTransport routes the handler's internally-constructed
// http.Client (which has no injectable Transport) at a local httptest server by
// swapping http.DefaultTransport for the duration of the test. The
// rewriteGitHubTransport only rewrites api.github.com / raw.githubusercontent.com
// hosts, so unrelated traffic is unaffected. Restored via t.Cleanup.
func installGitHubFixtureTransport(t *testing.T, handler http.HandlerFunc) *[]string {
	t.Helper()

	requests := &[]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.Header.Get("X-Test-Original-Host")+" "+r.URL.RequestURI())
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}

	prev := http.DefaultTransport
	http.DefaultTransport = &rewriteGitHubTransport{
		target: target,
		base:   prev,
		hosts: map[string]struct{}{
			"api.github.com":            {},
			"raw.githubusercontent.com": {},
		},
	}
	t.Cleanup(func() { http.DefaultTransport = prev })
	return requests
}

// githubSkillFixtureHandler serves the minimal GitHub API + raw surface for a
// single root-level skill repo: default branch, the SKILL.md body, and an empty
// contents listing (no supporting files). owner/repo/name select the fixture.
func githubSkillFixtureHandler(owner, repo, frontmatterName string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch r.URL.Path {
			case fmt.Sprintf("/repos/%s/%s", owner, repo):
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			case fmt.Sprintf("/repos/%s/%s/contents", owner, repo):
				writeJSON(w, http.StatusOK, []githubContentEntry{})
			default:
				http.NotFound(w, r)
			}
		case "raw.githubusercontent.com":
			if r.URL.Path == fmt.Sprintf("/%s/%s/main/SKILL.md", owner, repo) {
				w.Write([]byte("---\nname: " + frontmatterName + "\ndescription: fixture skill\n---\nbody"))
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}

func skillOriginByID(t *testing.T, skillID string) string {
	t.Helper()
	var origin string
	if err := testPool.QueryRow(context.Background(),
		`SELECT origin FROM skill WHERE id = $1`, skillID,
	).Scan(&origin); err != nil {
		t.Fatalf("load skill origin: %v", err)
	}
	return origin
}

func countSkillsByOrigin(t *testing.T, origin string) int {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM skill WHERE workspace_id = $1 AND origin = $2`, testWorkspaceID, origin,
	).Scan(&n); err != nil {
		t.Fatalf("count skills by origin: %v", err)
	}
	return n
}

// TestImportSkillSameURLReuses verifies that importing the same source URL
// twice reuses the first skill (origin identity) instead of creating a
// duplicate row or returning 409.
func TestImportSkillSameURLReuses(t *testing.T) {
	owner, repo := "acme", "reuse-"+strings.ToLower(strings.ReplaceAll(t.Name(), "_", "-"))
	installGitHubFixtureTransport(t, githubSkillFixtureHandler(owner, repo, "reuse-skill"))

	importURL := fmt.Sprintf("https://github.com/%s/%s", owner, repo)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE workspace_id = $1 AND origin = $2`, testWorkspaceID, importURL)
	})

	// First import → 201 Created.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/skills/import?workspace_id="+testWorkspaceID, map[string]any{"url": importURL})
	testHandler.ImportSkill(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first import: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var first SkillWithFilesResponse
	if err := json.NewDecoder(w.Body).Decode(&first); err != nil {
		t.Fatalf("decode first import: %v", err)
	}
	if got := skillOriginByID(t, first.ID); got != importURL {
		t.Fatalf("first import origin = %q, want %q", got, importURL)
	}

	// Second import of the SAME url → reuse, not duplicate. Returns 200 with the
	// same skill id, and no new row exists.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/skills/import?workspace_id="+testWorkspaceID, map[string]any{"url": importURL})
	testHandler.ImportSkill(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("second import: expected 200 (reuse), got %d: %s", w.Code, w.Body.String())
	}
	var second SkillWithFilesResponse
	if err := json.NewDecoder(w.Body).Decode(&second); err != nil {
		t.Fatalf("decode second import: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second import returned id %q, want reused %q", second.ID, first.ID)
	}
	if n := countSkillsByOrigin(t, importURL); n != 1 {
		t.Fatalf("expected exactly 1 skill row for origin, got %d", n)
	}
}

// TestImportSkillDifferentURLSameNameCoexists verifies that importing a second
// skill whose frontmatter name collides with an existing skill but whose source
// URL differs no longer 409s — they coexist as two distinct skills.
func TestImportSkillDifferentURLSameNameCoexists(t *testing.T) {
	sharedName := "collide-" + strings.ToLower(strings.ReplaceAll(t.Name(), "_", "-"))

	urlA := fmt.Sprintf("https://github.com/acme/%s-a", sharedName)
	urlB := fmt.Sprintf("https://github.com/acme/%s-b", sharedName)
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM skill WHERE workspace_id = $1 AND origin = ANY($2)`,
			testWorkspaceID, []string{urlA, urlB})
	})

	// Both repos resolve to the SAME frontmatter name but live at different URLs.
	installGitHubFixtureTransport(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("X-Test-Original-Host") {
		case "api.github.com":
			switch {
			case strings.HasSuffix(r.URL.Path, "/contents"):
				writeJSON(w, http.StatusOK, []githubContentEntry{})
			default:
				writeJSON(w, http.StatusOK, map[string]any{"default_branch": "main"})
			}
		case "raw.githubusercontent.com":
			if strings.HasSuffix(r.URL.Path, "/main/SKILL.md") {
				w.Write([]byte("---\nname: " + sharedName + "\ndescription: fixture\n---\nbody"))
				return
			}
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/skills/import?workspace_id="+testWorkspaceID, map[string]any{"url": urlA})
	testHandler.ImportSkill(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("import A: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var skillA SkillWithFilesResponse
	json.NewDecoder(w.Body).Decode(&skillA)

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/skills/import?workspace_id="+testWorkspaceID, map[string]any{"url": urlB})
	testHandler.ImportSkill(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("import B (same name, different URL): expected 201 (coexist), got %d: %s", w.Code, w.Body.String())
	}
	var skillB SkillWithFilesResponse
	json.NewDecoder(w.Body).Decode(&skillB)

	if skillA.ID == skillB.ID {
		t.Fatalf("same-name different-URL imports collapsed to one skill id %q", skillA.ID)
	}
	if skillA.Name != sharedName || skillB.Name != sharedName {
		t.Fatalf("expected both names %q, got %q and %q", sharedName, skillA.Name, skillB.Name)
	}
	if origin := skillOriginByID(t, skillA.ID); origin != urlA {
		t.Fatalf("skill A origin = %q, want %q", origin, urlA)
	}
	if origin := skillOriginByID(t, skillB.ID); origin != urlB {
		t.Fatalf("skill B origin = %q, want %q", origin, urlB)
	}
}

// TestCreateSkillDuplicateLocalNameConflicts verifies that two hand-authored
// skills with the same name still collide with 409 (origin = "local:"+name).
func TestCreateSkillDuplicateLocalNameConflicts(t *testing.T) {
	name := "manual-dup-" + fmt.Sprintf("%d", time.Now().UnixNano())
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE workspace_id = $1 AND origin = $2`, testWorkspaceID, "local:"+name)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/skills?workspace_id="+testWorkspaceID, map[string]any{
		"name":    name,
		"content": "first body",
	})
	testHandler.CreateSkill(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first manual create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var first SkillWithFilesResponse
	json.NewDecoder(w.Body).Decode(&first)
	if origin := skillOriginByID(t, first.ID); origin != "local:"+name {
		t.Fatalf("manual create origin = %q, want %q", origin, "local:"+name)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/skills?workspace_id="+testWorkspaceID, map[string]any{
		"name":    name,
		"content": "second body",
	})
	testHandler.CreateSkill(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate manual create: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}
