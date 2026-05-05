package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestProjectArchiveLifecycle covers the full archive → list-hidden →
// list-shown-on-toggle → restore round-trip.
func TestProjectArchiveLifecycle(t *testing.T) {
	// Create a project to archive.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Archive lifecycle project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)
	defer func() {
		// Hard-delete so the test row doesn't leak across runs.
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	}()

	if project.ArchivedAt != nil {
		t.Fatalf("freshly-created project should not be archived: %+v", project.ArchivedAt)
	}

	// 1. Archive.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/archive", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.ArchiveProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveProject: %d %s", w.Code, w.Body.String())
	}
	var archived ProjectResponse
	json.NewDecoder(w.Body).Decode(&archived)
	if archived.ArchivedAt == nil || *archived.ArchivedAt == "" {
		t.Fatalf("expected archived_at to be set after archive, got %+v", archived.ArchivedAt)
	}
	if archived.ArchivedBy == nil || *archived.ArchivedBy == "" {
		t.Fatalf("expected archived_by to be set after archive, got %+v", archived.ArchivedBy)
	}

	// 2. Default list — archived row hidden.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID, nil)
	testHandler.ListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjects: %d %s", w.Code, w.Body.String())
	}
	var defaultList struct {
		Projects []ProjectResponse `json:"projects"`
	}
	json.NewDecoder(w.Body).Decode(&defaultList)
	for _, p := range defaultList.Projects {
		if p.ID == project.ID {
			t.Fatalf("archived project still visible in default ListProjects: %+v", p)
		}
	}

	// 3. include_archived=true — archived row visible.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID+"&include_archived=true", nil)
	testHandler.ListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjects(include_archived): %d %s", w.Code, w.Body.String())
	}
	var inclList struct {
		Projects []ProjectResponse `json:"projects"`
	}
	json.NewDecoder(w.Body).Decode(&inclList)
	found := false
	for _, p := range inclList.Projects {
		if p.ID == project.ID {
			found = true
			if p.ArchivedAt == nil {
				t.Errorf("archived project surfaced via include_archived but archived_at is nil")
			}
		}
	}
	if !found {
		t.Fatalf("archived project missing from ListProjects(include_archived=true)")
	}

	// 4. Re-archiving an already-archived project must be a 409 (no-op).
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/archive", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.ArchiveProject(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("re-archive: expected 409, got %d %s", w.Code, w.Body.String())
	}

	// 5. Restore.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/restore", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.RestoreProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("RestoreProject: %d %s", w.Code, w.Body.String())
	}
	var restored ProjectResponse
	json.NewDecoder(w.Body).Decode(&restored)
	if restored.ArchivedAt != nil {
		t.Fatalf("expected archived_at cleared after restore, got %+v", restored.ArchivedAt)
	}
	if restored.ArchivedBy != nil {
		t.Fatalf("expected archived_by cleared after restore, got %+v", restored.ArchivedBy)
	}

	// 6. Restoring a non-archived project must be a 409.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/restore", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.RestoreProject(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("restore non-archived: expected 409, got %d %s", w.Code, w.Body.String())
	}

	// 7. After restore, project is back in the default list.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID, nil)
	testHandler.ListProjects(w, req)
	var defaultListAfter struct {
		Projects []ProjectResponse `json:"projects"`
	}
	json.NewDecoder(w.Body).Decode(&defaultListAfter)
	foundAfter := false
	for _, p := range defaultListAfter.Projects {
		if p.ID == project.ID {
			foundAfter = true
			break
		}
	}
	if !foundAfter {
		t.Fatalf("restored project missing from default ListProjects")
	}
}

// TestArchiveProjectKeepsIssueReference ensures archiving doesn't break
// the FK from issues to the project — a key reason to prefer archive
// over hard delete.
func TestArchiveProjectKeepsIssueReference(t *testing.T) {
	// Create project + issue pointing at it.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Project with issue",
	})
	testHandler.CreateProject(w, req)
	var project ProjectResponse
	json.NewDecoder(w.Body).Decode(&project)
	defer func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	}()

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":      "Issue tied to project",
		"project_id": project.ID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	defer func() {
		req := newRequest("DELETE", "/api/issues/"+issue.ID, nil)
		req = withURLParam(req, "id", issue.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), req)
	}()
	if issue.ProjectID == nil || *issue.ProjectID != project.ID {
		t.Fatalf("issue missing project_id: %+v", issue.ProjectID)
	}

	// Archive the project.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/archive", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.ArchiveProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ArchiveProject: %d %s", w.Code, w.Body.String())
	}

	// Issue's project_id MUST still point at the archived project.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/issues/"+issue.ID, nil)
	req = withURLParam(req, "id", issue.ID)
	testHandler.GetIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetIssue after archive: %d %s", w.Code, w.Body.String())
	}
	var issueAfter IssueResponse
	json.NewDecoder(w.Body).Decode(&issueAfter)
	if issueAfter.ProjectID == nil || *issueAfter.ProjectID != project.ID {
		t.Fatalf("issue lost project_id after archive: %+v", issueAfter.ProjectID)
	}
}
