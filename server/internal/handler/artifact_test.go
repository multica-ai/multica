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
