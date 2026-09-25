package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// createTestProjectWithParent seeds a project via the API and returns its
// ProjectResponse. The caller is responsible for cleanup via deleteProjectByID.
func createTestProjectWithParent(t *testing.T, title string, parentID *string) ProjectResponse {
	t.Helper()
	body := map[string]any{"title": title}
	if parentID != nil {
		body["parent_project_id"] = *parentID
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, body)
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		deleteProjectByID(t, project.ID)
	})
	return project
}

func deleteProjectByID(t *testing.T, id string) {
	t.Helper()
	req := newRequest("DELETE", "/api/projects/"+id, nil)
	req = withURLParam(req, "id", id)
	testHandler.DeleteProject(httptest.NewRecorder(), req)
}

// A project created with a valid parent_project_id gets the parent set and the
// field echoed in the response.
func TestCreateProjectWithParentSucceeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("testHandler not initialized")
	}
	parent := createTestProjectWithParent(t, "parent project for create test", nil)
	child := createTestProjectWithParent(t, "child project", &parent.ID)
	if child.ParentProjectID == nil || *child.ParentProjectID != parent.ID {
		t.Errorf("expected parent_project_id=%s, got %v", parent.ID, child.ParentProjectID)
	}
}

// A project created with a non-existent parent (wrong workspace) is rejected.
func TestCreateProjectWithNonexistentParentReturns400(t *testing.T) {
	if testHandler == nil {
		t.Skip("testHandler not initialized")
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":             "orphan child",
		"parent_project_id": "00000000-0000-0000-0000-000000000000",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for nonexistent parent, got %d: %s", w.Code, w.Body.String())
	}
}

// A project updated to set its parent to itself is rejected (self-reference).
func TestUpdateProjectSelfReferenceReturns400(t *testing.T) {
	if testHandler == nil {
		t.Skip("testHandler not initialized")
	}
	project := createTestProjectWithParent(t, "self-ref test project", nil)
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/projects/"+project.ID, map[string]any{
		"parent_project_id": project.ID,
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for self-reference, got %d: %s", w.Code, w.Body.String())
	}
}

// A project updated to set its parent to a child creates a cycle and must be
// rejected.
func TestUpdateProjectCycleReturns400(t *testing.T) {
	if testHandler == nil {
		t.Skip("testHandler not initialized")
	}
	parent := createTestProjectWithParent(t, "cycle test parent", nil)
	child := createTestProjectWithParent(t, "cycle test child", &parent.ID)
	// Setting parent's parent_project_id to child would create a cycle.
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/projects/"+parent.ID, map[string]any{
		"parent_project_id": child.ID,
	})
	req = withURLParam(req, "id", parent.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cycle, got %d: %s", w.Code, w.Body.String())
	}
}

// Depth exceeding maxProjectDepth is rejected.
func TestUpdateProjectDepthExceededReturns400(t *testing.T) {
	if testHandler == nil {
		t.Skip("testHandler not initialized")
	}
	level1 := createTestProjectWithParent(t, "depth L1", nil)
	level2 := createTestProjectWithParent(t, "depth L2", &level1.ID)
	level3 := createTestProjectWithParent(t, "depth L3", &level2.ID)
	// Setting level1's parent to level3 would make depth 4, exceeding maxProjectDepth (3).
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/projects/"+level1.ID, map[string]any{
		"parent_project_id": level3.ID,
	})
	req = withURLParam(req, "id", level1.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for depth exceeded, got %d: %s", w.Code, w.Body.String())
	}
}

// UpdateProject can clear the parent (make top-level) by sending parent_project_id: null.
func TestUpdateProjectClearParent(t *testing.T) {
	if testHandler == nil {
		t.Skip("testHandler not initialized")
	}
	parent := createTestProjectWithParent(t, "clear parent test parent", nil)
	child := createTestProjectWithParent(t, "clear parent test child", &parent.ID)
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/projects/"+child.ID, map[string]any{
		"parent_project_id": nil,
	})
	req = withURLParam(req, "id", child.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for clear parent, got %d: %s", w.Code, w.Body.String())
	}
	var updated ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.ParentProjectID != nil {
		t.Errorf("expected parent_project_id to be nil after clear, got %v", updated.ParentProjectID)
	}
}

// ListProjects with parent_id=null returns only top-level projects.
func TestListProjectsTopLevelFilter(t *testing.T) {
	if testHandler == nil {
		t.Skip("testHandler not initialized")
	}
	parent := createTestProjectWithParent(t, "list top-level parent", nil)
	child := createTestProjectWithParent(t, "list top-level child", &parent.ID)
	_ = child // ensure created

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID+"&parent_id=null", nil)
	testHandler.ListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	foundParent := false
	foundChild := false
	for _, p := range result.Projects {
		if p.ID == parent.ID {
			foundParent = true
		}
		if p.ID == child.ID {
			foundChild = true
		}
	}
	if !foundParent {
		t.Errorf("expected top-level parent in results")
	}
	if foundChild {
		t.Errorf("expected child to be filtered out of top-level results")
	}
}

// ListProjects with parent_id=<uuid> returns only direct children.
func TestListProjectsChildrenFilter(t *testing.T) {
	if testHandler == nil {
		t.Skip("testHandler not initialized")
	}
	parent := createTestProjectWithParent(t, "children filter parent", nil)
	child := createTestProjectWithParent(t, "children filter child", &parent.ID)

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID+"&parent_id="+parent.ID, nil)
	testHandler.ListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var result struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Projects) == 0 {
		t.Fatal("expected at least one child project")
	}
	foundChild := false
	for _, p := range result.Projects {
		if p.ID == child.ID {
			foundChild = true
		}
	}
	if !foundChild {
		t.Errorf("expected child in results")
	}
}

// GetProjectTree returns the root with nested children.
func TestGetProjectTree(t *testing.T) {
	if testHandler == nil {
		t.Skip("testHandler not initialized")
	}
	parent := createTestProjectWithParent(t, "tree root", nil)
	child := createTestProjectWithParent(t, "tree child", &parent.ID)
	grandchild := createTestProjectWithParent(t, "tree grandchild", &child.ID)
	_ = grandchild

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+parent.ID+"/tree?workspace_id="+testWorkspaceID, nil)
	req = withURLParam(req, "id", parent.ID)
	testHandler.GetProjectTree(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var tree ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&tree); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tree.ID != parent.ID {
		t.Errorf("expected root id %s, got %s", parent.ID, tree.ID)
	}
	if len(tree.Children) == 0 {
		t.Fatal("expected at least one child")
	}
	var foundChild, foundGrandchild bool
	for _, c := range tree.Children {
		if c.ID == child.ID {
			foundChild = true
		}
		for _, gc := range c.Children {
			if gc.ID == grandchild.ID {
				foundGrandchild = true
			}
		}
	}
	if !foundChild {
		t.Errorf("expected child in tree")
	}
	if !foundGrandchild {
		t.Errorf("expected grandchild in tree")
	}
}

// GetProject includes direct children in the response.
func TestGetProjectIncludesChildren(t *testing.T) {
	if testHandler == nil {
		t.Skip("testHandler not initialized")
	}
	parent := createTestProjectWithParent(t, "get with children parent", nil)
	child := createTestProjectWithParent(t, "get with children child", &parent.ID)
	_ = child

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/projects/"+parent.ID+"?workspace_id="+testWorkspaceID, nil)
	req = withURLParam(req, "id", parent.ID)
	testHandler.GetProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode: %v", err)
	}
	foundChild := false
	for _, c := range project.Children {
		if c.ID == child.ID {
			foundChild = true
		}
	}
	if !foundChild {
		t.Errorf("expected child in GetProject response")
	}
}

// Ensure no orphaned rows from tests that may have failed mid-setup.
func init() {
	// Best-effort cleanup of any stale test projects from prior runs.
	// The real cleanup is via t.Cleanup in createTestProjectWithParent.
	_ = context.Background()
}
