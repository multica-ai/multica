package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/multica-ai/multica/server/internal/agenttmpl"
)

// installTestTemplate swaps the package-level agentTemplates registry for one
// built from in-memory JSON, restoring the real catalog on cleanup. Each skill
// ref is (source_url, cached_name) — cached_name is display-only now, so tests
// can deliberately desync it from origin to prove dedup keys on the URL.
func installTestTemplate(t *testing.T, slug string, skills []agenttmpl.TemplateSkillRef) {
	t.Helper()

	type jsonSkill struct {
		SourceURL         string `json:"source_url"`
		CachedName        string `json:"cached_name,omitempty"`
		CachedDescription string `json:"cached_description,omitempty"`
	}
	js := make([]jsonSkill, 0, len(skills))
	for _, s := range skills {
		js = append(js, jsonSkill{SourceURL: s.SourceURL, CachedName: s.CachedName, CachedDescription: s.CachedDescription})
	}
	body, err := json.Marshal(map[string]any{
		"slug":         slug,
		"name":         "Test Template " + slug,
		"description":  "fixture template",
		"instructions": "do the thing",
		"skills":       js,
	})
	if err != nil {
		t.Fatalf("marshal template: %v", err)
	}

	fsys := fstest.MapFS{
		"templates/" + slug + ".json": &fstest.MapFile{Data: body},
	}
	reg, err := agenttmpl.LoadFromFS(fsys, "templates")
	if err != nil {
		t.Fatalf("LoadFromFS: %v", err)
	}

	prev := agentTemplates
	agentTemplates = reg
	t.Cleanup(func() { agentTemplates = prev })
}

func createFromTemplate(t *testing.T, slug, agentName string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/agents/from-template?workspace_id="+testWorkspaceID, map[string]any{
		"template_slug": slug,
		"name":          agentName,
		"runtime_id":    handlerTestRuntimeID(t),
	})
	testHandler.CreateAgentFromTemplate(w, req)
	return w
}

func cleanupAgentByName(t *testing.T, name string) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name)
	})
}

func cleanupSkillByOrigin(t *testing.T, origin string) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE workspace_id = $1 AND origin = $2`, testWorkspaceID, origin)
	})
}

// seedWorkspaceSkill inserts a skill row directly with an explicit origin and
// returns its id. Used to pre-stage workspace skills for reuse/non-reuse tests.
func seedWorkspaceSkill(t *testing.T, name, origin string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by, origin)
		VALUES ($1, $2, 'seed', 'seed body', '{}'::jsonb, $3, $4)
		RETURNING id
	`, testWorkspaceID, name, testUserID, origin).Scan(&id); err != nil {
		t.Fatalf("seed workspace skill: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, id)
	})
	return id
}

// TestCreateAgentFromTemplateHappyPath imports the template's skill, creates the
// agent, and binds the skill to it.
func TestCreateAgentFromTemplateHappyPath(t *testing.T) {
	owner, repo := "acme", "tmpl-happy-"+fmt.Sprintf("%d", time.Now().UnixNano())
	sourceURL := fmt.Sprintf("https://github.com/%s/%s", owner, repo)
	installGitHubFixtureTransport(t, githubSkillFixtureHandler(owner, repo, "happy-skill"))

	slug := "tmpl-happy"
	installTestTemplate(t, slug, []agenttmpl.TemplateSkillRef{{SourceURL: sourceURL, CachedName: "happy-skill"}})

	agentName := "happy-agent-" + fmt.Sprintf("%d", time.Now().UnixNano())
	cleanupAgentByName(t, agentName)
	cleanupSkillByOrigin(t, sourceURL)

	w := createFromTemplate(t, slug, agentName)
	if w.Code != http.StatusCreated {
		t.Fatalf("create-from-template: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateAgentFromTemplateResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.ImportedSkillIDs) != 1 {
		t.Fatalf("imported_skill_ids = %v, want exactly 1", resp.ImportedSkillIDs)
	}
	if len(resp.ReusedSkillIDs) != 0 {
		t.Fatalf("reused_skill_ids = %v, want empty", resp.ReusedSkillIDs)
	}
	if origin := skillOriginByID(t, resp.ImportedSkillIDs[0]); origin != sourceURL {
		t.Fatalf("imported skill origin = %q, want %q", origin, sourceURL)
	}
	// Skill is bound to the agent.
	assertAgentSkillRowCount(t, resp.Agent.ID, 1)
	if len(resp.Agent.Skills) != 1 || resp.Agent.Skills[0].ID != resp.ImportedSkillIDs[0] {
		t.Fatalf("agent.skills = %+v, want the imported skill bound", resp.Agent.Skills)
	}
}

// TestCreateAgentFromTemplateReusesSameOrigin pre-stages a workspace skill with
// the SAME origin (source URL) as the template's skill. It must be reused
// without a fetch (the fixture transport is intentionally absent, so any fetch
// attempt would fail the test by erroring the import).
func TestCreateAgentFromTemplateReusesSameOrigin(t *testing.T) {
	sourceURL := "https://github.com/acme/tmpl-reuse-" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Pre-existing workspace skill with the matching origin.
	existingID := seedWorkspaceSkill(t, "preexisting-name", sourceURL)

	slug := "tmpl-reuse"
	installTestTemplate(t, slug, []agenttmpl.TemplateSkillRef{{SourceURL: sourceURL, CachedName: "anything"}})

	agentName := "reuse-agent-" + fmt.Sprintf("%d", time.Now().UnixNano())
	cleanupAgentByName(t, agentName)

	w := createFromTemplate(t, slug, agentName)
	if w.Code != http.StatusCreated {
		t.Fatalf("create-from-template: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateAgentFromTemplateResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.ImportedSkillIDs) != 0 {
		t.Fatalf("imported_skill_ids = %v, want empty (reuse)", resp.ImportedSkillIDs)
	}
	if len(resp.ReusedSkillIDs) != 1 || resp.ReusedSkillIDs[0] != existingID {
		t.Fatalf("reused_skill_ids = %v, want [%s]", resp.ReusedSkillIDs, existingID)
	}
	assertAgentSkillRowCount(t, resp.Agent.ID, 1)
	// No duplicate skill row was created for this origin.
	if n := countSkillsByOrigin(t, sourceURL); n != 1 {
		t.Fatalf("expected 1 skill row for origin, got %d", n)
	}
}

// TestCreateAgentFromTemplateSameNameDifferentOriginNotReused pre-stages a
// workspace skill with the SAME NAME as the template's skill but a DIFFERENT
// origin. The template's own skill must be imported and bound — the same-name
// local skill must NOT be reused.
func TestCreateAgentFromTemplateSameNameDifferentOriginNotReused(t *testing.T) {
	owner, repo := "acme", "tmpl-name-"+fmt.Sprintf("%d", time.Now().UnixNano())
	sourceURL := fmt.Sprintf("https://github.com/%s/%s", owner, repo)
	sharedName := "shared-skill-name"

	installGitHubFixtureTransport(t, githubSkillFixtureHandler(owner, repo, sharedName))

	// Pre-existing workspace skill that shares the NAME but has a local origin.
	localID := seedWorkspaceSkill(t, sharedName, "local:"+sharedName)

	slug := "tmpl-name"
	installTestTemplate(t, slug, []agenttmpl.TemplateSkillRef{{SourceURL: sourceURL, CachedName: sharedName}})

	agentName := "name-agent-" + fmt.Sprintf("%d", time.Now().UnixNano())
	cleanupAgentByName(t, agentName)
	cleanupSkillByOrigin(t, sourceURL)

	w := createFromTemplate(t, slug, agentName)
	if w.Code != http.StatusCreated {
		t.Fatalf("create-from-template: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp CreateAgentFromTemplateResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.ImportedSkillIDs) != 1 {
		t.Fatalf("imported_skill_ids = %v, want exactly 1 (template skill imported, local not reused)", resp.ImportedSkillIDs)
	}
	if resp.ImportedSkillIDs[0] == localID {
		t.Fatalf("imported id equals the pre-seeded same-name local skill %q — must not reuse across origins", localID)
	}
	for _, reused := range resp.ReusedSkillIDs {
		if reused == localID {
			t.Fatalf("reused the same-name local skill %q despite different origin", localID)
		}
	}
	if origin := skillOriginByID(t, resp.ImportedSkillIDs[0]); origin != sourceURL {
		t.Fatalf("imported skill origin = %q, want %q", origin, sourceURL)
	}
	// Two skills now share the name but have distinct origins.
	var sameNameCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM skill WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, sharedName,
	).Scan(&sameNameCount); err != nil {
		t.Fatalf("count same-name skills: %v", err)
	}
	if sameNameCount != 2 {
		t.Fatalf("expected 2 same-name skills (local + imported), got %d", sameNameCount)
	}
}

// TestCreateAgentFromTemplateFailingSkillRollsBack verifies that a template skill
// whose source is unreachable yields 422 and creates no agent and no skill.
func TestCreateAgentFromTemplateFailingSkillRollsBack(t *testing.T) {
	owner, repo := "acme", "tmpl-fail-"+fmt.Sprintf("%d", time.Now().UnixNano())
	failURL := fmt.Sprintf("https://github.com/%s/%s", owner, repo)

	// Transport that 404s every GitHub request so the fetch fails for real.
	installGitHubFixtureTransport(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	slug := "tmpl-fail"
	installTestTemplate(t, slug, []agenttmpl.TemplateSkillRef{{SourceURL: failURL, CachedName: "fail-skill"}})

	agentName := "fail-agent-" + fmt.Sprintf("%d", time.Now().UnixNano())
	cleanupAgentByName(t, agentName)
	cleanupSkillByOrigin(t, failURL)

	w := createFromTemplate(t, slug, agentName)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("create-from-template with failing skill: expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var fail fetchFailureResponse
	if err := json.NewDecoder(w.Body).Decode(&fail); err != nil {
		t.Fatalf("decode failure response: %v", err)
	}
	if len(fail.FailedURLs) == 0 || !strings.Contains(strings.Join(fail.FailedURLs, ","), failURL) {
		t.Fatalf("failed_urls = %v, want it to include %q", fail.FailedURLs, failURL)
	}

	// No agent was created (rollback).
	var agentCount int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, agentName,
	).Scan(&agentCount); err != nil {
		t.Fatalf("count agents: %v", err)
	}
	if agentCount != 0 {
		t.Fatalf("expected no agent after failed template create, got %d", agentCount)
	}
	// No skill was created for the failing origin.
	if n := countSkillsByOrigin(t, failURL); n != 0 {
		t.Fatalf("expected no skill row for failing origin, got %d", n)
	}
}
