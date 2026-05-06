package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectSettingsHooksAfterCreateRoundTrip(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Settings hook project",
		"settings": map[string]any{
			"hooks": map[string]any{
				"after_create": "git clone https://github.com/example/repo.git .",
			},
		},
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

	settings := project.Settings.(map[string]any)
	hooks := settings["hooks"].(map[string]any)
	if got := hooks["after_create"]; got != "git clone https://github.com/example/repo.git ." {
		t.Fatalf("create settings hooks.after_create = %q", got)
	}

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID, map[string]any{
		"settings": map[string]any{
			"hooks": map[string]any{
				"after_create": "corepack pnpm install --frozen-lockfile",
			},
		},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProject: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode UpdateProject: %v", err)
	}
	settings = updated.Settings.(map[string]any)
	hooks = settings["hooks"].(map[string]any)
	if got := hooks["after_create"]; got != "corepack pnpm install --frozen-lockfile" {
		t.Fatalf("updated settings hooks.after_create = %q", got)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.GetProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetProject: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var fetched ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode GetProject: %v", err)
	}
	settings = fetched.Settings.(map[string]any)
	hooks = settings["hooks"].(map[string]any)
	if got := hooks["after_create"]; got != "corepack pnpm install --frozen-lockfile" {
		t.Fatalf("fetched settings hooks.after_create = %q", got)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID, nil)
	testHandler.ListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjects: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var listed struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listed); err != nil {
		t.Fatalf("decode ListProjects: %v", err)
	}
	found := false
	for _, p := range listed.Projects {
		if p.ID != project.ID {
			continue
		}
		found = true
		settings = p.Settings.(map[string]any)
		hooks = settings["hooks"].(map[string]any)
		if got := hooks["after_create"]; got != "corepack pnpm install --frozen-lockfile" {
			t.Fatalf("listed settings hooks.after_create = %q", got)
		}
	}
	if !found {
		t.Fatalf("project %s missing from list response", project.ID)
	}

	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/search/projects?workspace_id="+testWorkspaceID+"&q=Settings", nil)
	testHandler.SearchProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("SearchProjects: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var searched struct {
		Projects []SearchProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&searched); err != nil {
		t.Fatalf("decode SearchProjects: %v", err)
	}
	found = false
	for _, p := range searched.Projects {
		if p.ID != project.ID {
			continue
		}
		found = true
		settings = p.Settings.(map[string]any)
		hooks = settings["hooks"].(map[string]any)
		if got := hooks["after_create"]; got != "corepack pnpm install --frozen-lockfile" {
			t.Fatalf("searched settings hooks.after_create = %q", got)
		}
	}
	if !found {
		t.Fatalf("project %s missing from search response", project.ID)
	}
}

func TestProjectSettingsRejectMalformedHooks(t *testing.T) {
	for name, body := range map[string]map[string]any{
		"settings array": {
			"title":    "Bad settings",
			"settings": []any{"not", "object"},
		},
		"hooks string": {
			"title":    "Bad hooks",
			"settings": map[string]any{"hooks": "nope"},
		},
		"after_create number": {
			"title": "Bad after_create",
			"settings": map[string]any{
				"hooks": map[string]any{"after_create": 42},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, body)
			testHandler.CreateProject(w, req)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("CreateProject: expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestProjectSettingsHooksRequireAdminRole(t *testing.T) {
	memberUserID := createRuntimeLocalSkillTestMember(t, "member")

	w := httptest.NewRecorder()
	req := newRequestAsUser(memberUserID, http.MethodPost, "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Member hook project",
		"settings": map[string]any{
			"hooks": map[string]any{
				"after_create": "echo no",
			},
		},
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateProject with hook as member: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Owner-created hook target",
		"settings": map[string]any{
			"hooks": map[string]any{
				"after_create": "echo yes",
			},
		},
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

	w = httptest.NewRecorder()
	req = newRequestAsUser(memberUserID, http.MethodPut, "/api/projects/"+project.ID, map[string]any{
		"settings": map[string]any{},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateProject clearing hook as member: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAsUser(memberUserID, http.MethodPut, "/api/projects/"+project.ID, map[string]any{
		"settings": map[string]any{
			"hooks": map[string]any{
				"after_create": "echo no",
			},
		},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("UpdateProject with hook as member: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

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
	req = withURLParams(req, "id", project.ID, "resourceId", created.ID)
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

func TestCreateProjectAttachesResources(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Project with bundled resources",
		"resources": []map[string]any{
			{
				"resource_type": "github_repo",
				"resource_ref":  map[string]any{"url": "https://github.com/multica-ai/multica"},
			},
		},
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject with resources: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ID        string                    `json:"id"`
		Resources []ProjectResourceResponse `json:"resources"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	defer func() {
		r := newRequest("DELETE", "/api/projects/"+resp.ID, nil)
		r = withURLParam(r, "id", resp.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	}()

	if len(resp.Resources) != 1 || resp.Resources[0].ResourceType != "github_repo" {
		t.Fatalf("response resources mismatch: %+v", resp.Resources)
	}
}

func TestCreateProjectRollsBackOnInvalidResource(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Project that should not exist",
		"resources": []map[string]any{
			{
				"resource_type": "github_repo",
				"resource_ref":  map[string]any{"url": "not-a-url"},
			},
		},
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateProject with invalid resource: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Confirm no project survived (transactional rollback). Listing all projects
	// in the workspace and checking for the title is enough.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID, nil)
	testHandler.ListProjects(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjects: %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Projects []ProjectResponse `json:"projects"`
	}
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, p := range list.Projects {
		if p.Title == "Project that should not exist" {
			t.Errorf("invalid resource should have rolled back project create, but found %s", p.ID)
		}
	}
}
