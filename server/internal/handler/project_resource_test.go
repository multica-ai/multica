package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectResourceLifecycle(t *testing.T) {
	// Create a project to attach resources to.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Resource lifecycle project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	defer func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	}()

	// Attach a github_repo resource.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "github_repo",
		"resource_ref":  map[string]any{"url": "https://github.com/multica-ai/multica"},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectResource: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created ProjectResourceResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode CreateProjectResource: %v", err)
	}
	if created.ResourceType != "github_repo" {
		t.Errorf("created.ResourceType = %q, want github_repo", created.ResourceType)
	}
	var ref struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(created.ResourceRef, &ref); err != nil {
		t.Fatalf("decode resource_ref: %v", err)
	}
	if ref.URL != "https://github.com/multica-ai/multica" {
		t.Errorf("created.ResourceRef.url = %q", ref.URL)
	}

	// Listing must include the new resource.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+project.ID+"/resources", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.ListProjectResources(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjectResources: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Resources []ProjectResourceResponse `json:"resources"`
		Total     int                       `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listResp.Total != 1 || len(listResp.Resources) != 1 {
		t.Fatalf("list returned %d resources, want 1", listResp.Total)
	}
	if listResp.Resources[0].ID != created.ID {
		t.Errorf("list[0].ID = %q, want %q", listResp.Resources[0].ID, created.ID)
	}

	// Duplicate attach must conflict (UNIQUE on project_id + type + ref).
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "github_repo",
		"resource_ref":  map[string]any{"url": "https://github.com/multica-ai/multica"},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate CreateProjectResource: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// Invalid URL must reject at the validator level.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "github_repo",
		"resource_ref":  map[string]any{"url": "not-a-url"},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid URL: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Unknown resource_type must reject.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "unknown_type",
		"resource_ref":  map[string]any{"foo": "bar"},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown type: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Delete the resource.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/projects/"+project.ID+"/resources/"+created.ID, nil)
	req = withURLParam(req, "id", project.ID)
	req = withURLParam(req, "resourceId", created.ID)
	testHandler.DeleteProjectResource(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteProjectResource: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	// After deletion the list should be empty.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+project.ID+"/resources", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.ListProjectResources(w, req)
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode post-delete list: %v", err)
	}
	if listResp.Total != 0 {
		t.Errorf("post-delete list: total = %d, want 0", listResp.Total)
	}
}

