package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func createTestProject(t *testing.T, title string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var p ProjectResponse
	json.NewDecoder(w.Body).Decode(&p)
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/projects/"+p.ID, nil)
		req = withURLParam(req, "id", p.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})
	return p.ID
}

func createTestIssue(t *testing.T, title string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  title,
		"status": "todo",
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var i IssueResponse
	json.NewDecoder(w.Body).Decode(&i)
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/issues/"+i.ID, nil)
		req = withURLParam(req, "id", i.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), req)
	})
	return i.ID
}

func TestArtifact_CreateOnIssue(t *testing.T) {
	issueID := createTestIssue(t, "Issue for artifact test")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/artifacts", map[string]any{
		"kind":     "report",
		"title":    "Investigation results",
		"body":     "# Findings\n\nLooks fine.",
		"issue_id": issueID,
	})
	testHandler.CreateArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateArtifact: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var got ArtifactResponse
	json.NewDecoder(w.Body).Decode(&got)
	if got.Kind != "report" {
		t.Fatalf("expected kind report, got %s", got.Kind)
	}
	if got.IssueID == nil || *got.IssueID != issueID {
		t.Fatalf("expected issue_id %s, got %v", issueID, got.IssueID)
	}
	if got.ProjectID != nil {
		t.Fatalf("expected no project_id, got %v", got.ProjectID)
	}
	if got.AuthorType != "member" {
		t.Fatalf("expected author_type member, got %s", got.AuthorType)
	}

	// List artifacts on the issue.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issueID+"/artifacts", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.ListArtifactsForIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListArtifactsForIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list []ArtifactResponse
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(list))
	}
	if list[0].ID != got.ID {
		t.Fatalf("expected artifact id %s, got %s", got.ID, list[0].ID)
	}
}

func TestArtifact_CreateOnProject(t *testing.T) {
	projectID := createTestProject(t, "Project for artifact test")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/artifacts", map[string]any{
		"kind":       "decision",
		"title":      "ADR-001: Storage layer",
		"body":       "## Context\n## Decision\n## Consequences",
		"project_id": projectID,
	})
	testHandler.CreateArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateArtifact: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var got ArtifactResponse
	json.NewDecoder(w.Body).Decode(&got)
	if got.ProjectID == nil || *got.ProjectID != projectID {
		t.Fatalf("expected project_id %s, got %v", projectID, got.ProjectID)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+projectID+"/artifacts", nil)
	req = withURLParam(req, "id", projectID)
	testHandler.ListArtifactsForProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListArtifactsForProject: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var list []ArtifactResponse
	json.NewDecoder(w.Body).Decode(&list)
	if len(list) != 1 {
		t.Fatalf("expected 1 project artifact, got %d", len(list))
	}
}

func TestArtifact_RejectsInvalidKind(t *testing.T) {
	issueID := createTestIssue(t, "Issue for invalid kind test")
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/artifacts", map[string]any{
		"kind":     "garbage",
		"title":    "x",
		"body":     "x",
		"issue_id": issueID,
	})
	testHandler.CreateArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid kind, got %d: %s", w.Code, w.Body.String())
	}
}

func TestArtifact_RejectsBothScopes(t *testing.T) {
	issueID := createTestIssue(t, "Issue for dual scope")
	projectID := createTestProject(t, "Project for dual scope")
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/artifacts", map[string]any{
		"kind":       "note",
		"title":      "x",
		"body":       "x",
		"issue_id":   issueID,
		"project_id": projectID,
	})
	testHandler.CreateArtifact(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for dual scope, got %d: %s", w.Code, w.Body.String())
	}
}

func TestArtifact_UpdateAndDelete(t *testing.T) {
	projectID := createTestProject(t, "Project for update test")
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/artifacts", map[string]any{
		"kind":       "plan",
		"title":      "Original title",
		"body":       "Original body",
		"project_id": projectID,
	})
	testHandler.CreateArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create failed: %d %s", w.Code, w.Body.String())
	}
	var created ArtifactResponse
	json.NewDecoder(w.Body).Decode(&created)

	// Update title and body.
	newTitle := "Updated title"
	newBody := "Updated body"
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/artifacts/"+created.ID, map[string]any{
		"title": newTitle,
		"body":  newBody,
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateArtifact(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateArtifact: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated ArtifactResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.Title != newTitle || updated.Body != newBody {
		t.Fatalf("update did not persist: %+v", updated)
	}

	// Get reflects the update.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/artifacts/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.GetArtifact(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetArtifact: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var fetched ArtifactResponse
	json.NewDecoder(w.Body).Decode(&fetched)
	if fetched.Title != newTitle {
		t.Fatalf("fetch after update: expected %s, got %s", newTitle, fetched.Title)
	}

	// Delete.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/artifacts/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.DeleteArtifact(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteArtifact: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// Confirm gone.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/artifacts/"+created.ID, nil)
	req = withURLParam(req, "id", created.ID)
	testHandler.GetArtifact(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", w.Code)
	}
}

func TestArtifact_SearchByKindAndQuery(t *testing.T) {
	projectID := createTestProject(t, "Project for search test")

	// Seed: one report, one plan, one decision.
	for _, item := range []struct{ kind, title, body string }{
		{"report", "Q1 latency report", "Investigation findings"},
		{"plan", "Q2 plan", "Goal and tasks"},
		{"decision", "ADR-001", "Use Postgres"},
	} {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/artifacts", map[string]any{
			"kind":       item.kind,
			"title":      item.title,
			"body":       item.body,
			"project_id": projectID,
		})
		testHandler.CreateArtifact(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("seed create %s: %d %s", item.kind, w.Code, w.Body.String())
		}
	}

	// kind=plan should return only the plan.
	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/artifacts?kind=plan", nil)
	testHandler.SearchArtifacts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("search by kind: %d %s", w.Code, w.Body.String())
	}
	var byKind []ArtifactResponse
	json.NewDecoder(w.Body).Decode(&byKind)
	if len(byKind) != 1 || byKind[0].Kind != "plan" {
		t.Fatalf("expected 1 plan artifact, got %d %+v", len(byKind), byKind)
	}

	// q=Postgres should match the decision body.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/artifacts?q=Postgres", nil)
	testHandler.SearchArtifacts(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("search by q: %d %s", w.Code, w.Body.String())
	}
	var byQuery []ArtifactResponse
	json.NewDecoder(w.Body).Decode(&byQuery)
	if len(byQuery) != 1 || byQuery[0].Kind != "decision" {
		t.Fatalf("expected 1 decision matching 'Postgres', got %d %+v", len(byQuery), byQuery)
	}

	// scope=project should include all 3.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/artifacts?scope=project", nil)
	testHandler.SearchArtifacts(w, req)
	var byScope []ArtifactResponse
	json.NewDecoder(w.Body).Decode(&byScope)
	if len(byScope) < 3 {
		t.Fatalf("expected ≥3 project-scoped artifacts, got %d", len(byScope))
	}
}

func TestArtifact_MoveScope_PromoteIssueToProject(t *testing.T) {
	projectID := createTestProject(t, "Promotion target project")
	issueID := createTestIssue(t, "Issue with artifact to promote")

	// Create on issue.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/artifacts", map[string]any{
		"kind":     "report",
		"title":    "Worth promoting",
		"body":     "Useful beyond this issue",
		"issue_id": issueID,
	})
	testHandler.CreateArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", w.Code, w.Body.String())
	}
	var created ArtifactResponse
	json.NewDecoder(w.Body).Decode(&created)

	// Promote to project.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/artifacts/"+created.ID+"/scope", map[string]any{
		"project_id": projectID,
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateArtifactScope(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("promote: %d %s", w.Code, w.Body.String())
	}
	var moved ArtifactResponse
	json.NewDecoder(w.Body).Decode(&moved)
	if moved.IssueID != nil {
		t.Fatalf("expected issue_id cleared after promotion, got %v", moved.IssueID)
	}
	if moved.ProjectID == nil || *moved.ProjectID != projectID {
		t.Fatalf("expected project_id %s, got %v", projectID, moved.ProjectID)
	}

	// Demote to workspace (clear all scope).
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/artifacts/"+created.ID+"/scope", map[string]any{})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateArtifactScope(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("demote: %d %s", w.Code, w.Body.String())
	}
	var workspaceScoped ArtifactResponse
	json.NewDecoder(w.Body).Decode(&workspaceScoped)
	if workspaceScoped.ProjectID != nil || workspaceScoped.IssueID != nil {
		t.Fatalf("expected workspace scope (both nil), got project=%v issue=%v", workspaceScoped.ProjectID, workspaceScoped.IssueID)
	}
}

func TestArtifact_MoveScope_RejectsBothTargets(t *testing.T) {
	projectID := createTestProject(t, "Move-target project")
	issueID := createTestIssue(t, "Move-target issue")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/artifacts", map[string]any{
		"kind":       "note",
		"title":      "x",
		"body":       "x",
		"project_id": projectID,
	})
	testHandler.CreateArtifact(w, req)
	var a ArtifactResponse
	json.NewDecoder(w.Body).Decode(&a)

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/artifacts/"+a.ID+"/scope", map[string]any{
		"project_id": projectID,
		"issue_id":   issueID,
	})
	req = withURLParam(req, "id", a.ID)
	testHandler.UpdateArtifactScope(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for both scopes, got %d", w.Code)
	}
}

func TestArtifact_RequiresWorkspaceMembership(t *testing.T) {
	issueID := createTestIssue(t, "Issue for membership test")

	// Create as legitimate member first.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/artifacts", map[string]any{
		"kind":     "note",
		"title":    "Membership probe",
		"body":     "x",
		"issue_id": issueID,
	})
	testHandler.CreateArtifact(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("setup create: expected 201, got %d", w.Code)
	}
	var a ArtifactResponse
	json.NewDecoder(w.Body).Decode(&a)

	// Attempt to fetch with no user header (simulates outsider).
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/artifacts/"+a.ID, nil)
	req.Header.Del("X-User-ID")
	req = withURLParam(req, "id", a.ID)
	testHandler.GetArtifact(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without user, got %d: %s", w.Code, w.Body.String())
	}
}
