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

// TestProjectResourceAcceptsSSHRepoURLs covers GitHub issue #2484: SSH and
// scp-like git URLs must be accepted alongside https URLs, because workspace
// repos configured with an SSH remote previously got rejected when attached
// to a project.
func TestProjectResourceAcceptsSSHRepoURLs(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "SSH repo URL acceptance",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	defer func() {
		r := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		r = withURLParam(r, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	}()

	cases := []struct {
		name string
		url  string
	}{
		{"scp-like", "git@github.com:multica-ai/multica.git"},
		{"ssh-scheme", "ssh://git@github.com/multica-ai/multica.git"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
				"resource_type": "github_repo",
				"resource_ref":  map[string]any{"url": tc.url},
			})
			req = withURLParam(req, "id", project.ID)
			testHandler.CreateProjectResource(w, req)
			if w.Code != http.StatusCreated {
				t.Fatalf("CreateProjectResource(%s): expected 201, got %d: %s", tc.url, w.Code, w.Body.String())
			}
			var created ProjectResourceResponse
			if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
				t.Fatalf("decode: %v", err)
			}
			var ref struct {
				URL string `json:"url"`
			}
			if err := json.Unmarshal(created.ResourceRef, &ref); err != nil {
				t.Fatalf("decode resource_ref: %v", err)
			}
			if ref.URL != tc.url {
				t.Errorf("ref.url = %q, want %q", ref.URL, tc.url)
			}
		})
	}
}

func TestIsValidGitRepoURL(t *testing.T) {
	good := []string{
		"https://github.com/multica-ai/multica",
		"https://github.com/multica-ai/multica.git",
		"http://github.example.com/x/y",
		"ssh://git@github.com/multica-ai/multica.git",
		"ssh://git@github.com:22/multica-ai/multica.git",
		"git@github.com:multica-ai/multica.git",
		"git@gitlab.example.com:group/sub/repo.git",
	}
	bad := []string{
		"",
		"not-a-url",
		"github.com/multica-ai/multica", // no scheme, no scp-style colon
		"https://",                      // empty host
		"git@github.com",                // missing :path
		"git@:foo/bar",                  // missing host
		"git@github.com:",               // missing path
		"ftp://example.com/repo",        // unsupported scheme
		"file:///tmp/repo",              // unsupported scheme
		"some random text with spaces",
		"github.com:org/repo@branch",    // '@' after ':' belongs to the path, not user
		"foo:bar@baz",                   // '@' after ':' with no scheme
		":foo/bar",                      // leading ':' with no host
	}
	for _, s := range good {
		if !isValidGitRepoURL(s) {
			t.Errorf("isValidGitRepoURL(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if isValidGitRepoURL(s) {
			t.Errorf("isValidGitRepoURL(%q) = true, want false", s)
		}
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

// TestProjectResourceCountBreadcrumb asserts the resource_count breadcrumb
// surfaces on GetProject and ListProjects so agents know to call
// /api/projects/{id}/resources without inlining the sub-collection.
func TestProjectResourceCountBreadcrumb(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Resource count breadcrumb",
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
		r := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		r = withURLParam(r, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	}()

	getCount := func() int64 {
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.GetProject(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GetProject: %d %s", w.Code, w.Body.String())
		}
		var resp ProjectResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode GetProject: %v", err)
		}
		return resp.ResourceCount
	}
	if got := getCount(); got != 0 {
		t.Errorf("initial GetProject ResourceCount = %d, want 0", got)
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "github_repo",
		"resource_ref":  map[string]any{"url": "https://github.com/multica-ai/breadcrumb"},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectResource: %d %s", w.Code, w.Body.String())
	}

	if got := getCount(); got != 1 {
		t.Errorf("after attach GetProject ResourceCount = %d, want 1", got)
	}

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
		t.Fatalf("decode ListProjects: %v", err)
	}
	found := false
	for _, p := range list.Projects {
		if p.ID == project.ID {
			found = true
			if p.ResourceCount != 1 {
				t.Errorf("ListProjects[%s].ResourceCount = %d, want 1", p.ID, p.ResourceCount)
			}
			break
		}
	}
	if !found {
		t.Fatalf("project %s not found in ListProjects response", project.ID)
	}

	// UpdateProject must preserve the breadcrumb. A title-only PUT used to
	// reset resource_count to 0 because UpdateProject didn't reload the count.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID, map[string]any{
		"title": "Resource count breadcrumb (updated)",
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProject: %d %s", w.Code, w.Body.String())
	}
	var updated ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode UpdateProject: %v", err)
	}
	if updated.ResourceCount != 1 {
		t.Errorf("UpdateProject ResourceCount = %d, want 1", updated.ResourceCount)
	}
}

// TestCreateProjectWithResourcesEchoesCount asserts the create-with-resources
// echo carries resource_count matching the attached resources, so the HTTP
// response and the published project:created event agree.
func TestCreateProjectWithResourcesEchoesCount(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Create echo with resource_count",
		"resources": []map[string]any{
			{
				"resource_type": "github_repo",
				"resource_ref":  map[string]any{"url": "https://github.com/multica-ai/echo-count"},
			},
		},
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject with resources: %d %s", w.Code, w.Body.String())
	}
	var resp struct {
		ID            string                    `json:"id"`
		ResourceCount int64                     `json:"resource_count"`
		Resources     []ProjectResourceResponse `json:"resources"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	defer func() {
		r := newRequest("DELETE", "/api/projects/"+resp.ID, nil)
		r = withURLParam(r, "id", resp.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	}()
	if resp.ResourceCount != 1 || len(resp.Resources) != 1 {
		t.Errorf("CreateProject echo: resource_count=%d resources=%d, want 1/1", resp.ResourceCount, len(resp.Resources))
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

