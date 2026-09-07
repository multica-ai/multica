package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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
		"resource_ref": map[string]any{
			"url": "https://github.com/multica-ai/multica",
			"ref": "release/v2",
		},
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
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal(created.ResourceRef, &ref); err != nil {
		t.Fatalf("decode resource_ref: %v", err)
	}
	if ref.URL != "https://github.com/multica-ai/multica" {
		t.Errorf("created.ResourceRef.url = %q", ref.URL)
	}
	if ref.Ref != "release/v2" {
		t.Errorf("created.ResourceRef.ref = %q, want release/v2", ref.Ref)
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
		"resource_ref": map[string]any{
			"url": "https://github.com/multica-ai/multica",
			"ref": "release/v2",
		},
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
		"github.com:org/repo@branch", // '@' after ':' belongs to the path, not user
		"foo:bar@baz",                // '@' after ':' with no scheme
		":foo/bar",                   // leading ':' with no host
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

// TestProjectResourceLocalDirectoryLifecycle covers the full CRUD path for the
// local_directory resource type added in MUL-2662. Unlike github_repo, the
// ref schema requires local_path + daemon_id and forbids any path that isn't
// absolute. Two project-scoped resources pointing at the same daemon_id /
// local_path on different projects must be allowed — Bohan explicitly chose
// not to add a UNIQUE(daemon_id, local_path) constraint.
func TestProjectResourceLocalDirectoryLifecycle(t *testing.T) {
	createProject := func(title string) ProjectResponse {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
			"title": title,
		})
		testHandler.CreateProject(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateProject(%s): %d %s", title, w.Code, w.Body.String())
		}
		var p ProjectResponse
		if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
			t.Fatalf("decode CreateProject: %v", err)
		}
		return p
	}
	deleteProject := func(id string) {
		r := newRequest("DELETE", "/api/projects/"+id, nil)
		r = withURLParam(r, "id", id)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	}

	projectA := createProject("Local directory project A")
	defer deleteProject(projectA.ID)
	projectB := createProject("Local directory project B")
	defer deleteProject(projectB.ID)

	const (
		daemonID  = "daemon-aaaa-bbbb-cccc"
		localPath = "/Users/foo/work/my-game"
	)

	// Happy path: attach local_directory resource with label.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectA.ID+"/resources", map[string]any{
		"resource_type": "local_directory",
		"resource_ref": map[string]any{
			"local_path": localPath,
			"daemon_id":  daemonID,
			"label":      "Game Repo",
		},
	})
	req = withURLParam(req, "id", projectA.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectResource: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created ProjectResourceResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode CreateProjectResource: %v", err)
	}
	if created.ResourceType != "local_directory" {
		t.Errorf("ResourceType = %q, want local_directory", created.ResourceType)
	}
	var ref struct {
		LocalPath string `json:"local_path"`
		DaemonID  string `json:"daemon_id"`
		Label     string `json:"label"`
	}
	if err := json.Unmarshal(created.ResourceRef, &ref); err != nil {
		t.Fatalf("decode resource_ref: %v", err)
	}
	if ref.LocalPath != localPath || ref.DaemonID != daemonID || ref.Label != "Game Repo" {
		t.Errorf("ref = %+v, want {%q, %q, Game Repo}", ref, localPath, daemonID)
	}

	// Listing must include the new resource.
	w = httptest.NewRecorder()
	req = newRequest("GET", "/api/projects/"+projectA.ID+"/resources", nil)
	req = withURLParam(req, "id", projectA.ID)
	testHandler.ListProjectResources(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListProjectResources: %d %s", w.Code, w.Body.String())
	}
	var listResp struct {
		Resources []ProjectResourceResponse `json:"resources"`
		Total     int                       `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listResp.Total != 1 || listResp.Resources[0].ID != created.ID {
		t.Fatalf("list mismatch: %+v", listResp)
	}

	// Same (daemon_id, local_path) on a different project must succeed —
	// the design explicitly allows the same directory to back multiple
	// projects, contrast with github_repo's per-project UNIQUE check.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+projectB.ID+"/resources", map[string]any{
		"resource_type": "local_directory",
		"resource_ref": map[string]any{
			"local_path": localPath,
			"daemon_id":  daemonID,
		},
	})
	req = withURLParam(req, "id", projectB.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("same path on project B: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Duplicate attach on the same project must still conflict — the
	// UNIQUE(project_id, resource_type, resource_ref) row constraint
	// remains in effect.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+projectA.ID+"/resources", map[string]any{
		"resource_type": "local_directory",
		"resource_ref": map[string]any{
			"local_path": localPath,
			"daemon_id":  daemonID,
			"label":      "Game Repo",
		},
	})
	req = withURLParam(req, "id", projectA.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate on same project: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// Delete the resource on project A.
	w = httptest.NewRecorder()
	req = newRequest("DELETE", "/api/projects/"+projectA.ID+"/resources/"+created.ID, nil)
	req = withURLParams(req, "id", projectA.ID, "resourceId", created.ID)
	testHandler.DeleteProjectResource(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteProjectResource: expected 204, got %d: %s", w.Code, w.Body.String())
	}
}

// TestProjectResourceLocalDirectoryValidation pins the schema rejection
// surface for local_directory: missing path, missing daemon, relative paths,
// and malformed JSON must all return 400. These are the only client-visible
// errors agents will hit, so freezing them as tests prevents accidental
// loosening when someone touches the validator.
func TestProjectResourceLocalDirectoryValidation(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Local directory validation",
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
		ref  any
	}{
		{"missing local_path", map[string]any{"daemon_id": "d1"}},
		{"blank local_path", map[string]any{"local_path": "   ", "daemon_id": "d1"}},
		{"relative local_path", map[string]any{"local_path": "work/my-game", "daemon_id": "d1"}},
		{"home-shorthand path", map[string]any{"local_path": "~/work/my-game", "daemon_id": "d1"}},
		{"missing daemon_id", map[string]any{"local_path": "/Users/foo/work"}},
		{"blank daemon_id", map[string]any{"local_path": "/Users/foo/work", "daemon_id": ""}},
		{"wrong type in payload", map[string]any{"local_path": 42, "daemon_id": "d1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
				"resource_type": "local_directory",
				"resource_ref":  tc.ref,
			})
			req = withURLParam(req, "id", project.ID)
			testHandler.CreateProjectResource(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestIsAbsoluteLocalPath(t *testing.T) {
	good := []string{
		"/Users/foo/work",
		"/",
		"/a",
		`C:\Users\foo`,
		`C:/Users/foo`,
		`d:\code\repo`,
		`\\server\share\path`,
	}
	bad := []string{
		"",
		"work/my-game",
		"./relative",
		"../relative",
		"~/work",
		"C:relative",
		"C:",
		`\foo`,
		"file:///tmp",
	}
	for _, s := range good {
		if !isAbsoluteLocalPath(s) {
			t.Errorf("isAbsoluteLocalPath(%q) = false, want true", s)
		}
	}
	for _, s := range bad {
		if isAbsoluteLocalPath(s) {
			t.Errorf("isAbsoluteLocalPath(%q) = true, want false", s)
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

	var number int
	if err := testPool.QueryRow(context.Background(), `
		UPDATE workspace
		SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
		WHERE id = $1 RETURNING issue_counter
	`, testWorkspaceID).Scan(&number); err != nil {
		t.Fatalf("next issue number: %v", err)
	}
	var issueID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (
			workspace_id, creator_type, creator_id, title, status, priority,
			project_id, number, position
		)
		VALUES ($1, 'member', $2, 'Project update stats breadcrumb', 'done', 'none', $3, $4, 0)
		RETURNING id
	`, testWorkspaceID, testUserID, project.ID, number).Scan(&issueID); err != nil {
		t.Fatalf("create project issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
	})

	// UpdateProject must preserve the breadcrumb. A title-only PUT used to
	// reset derived counts to 0 because UpdateProject didn't reload them.
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
	if updated.IssueCount != 1 || updated.DoneCount != 1 {
		t.Errorf("UpdateProject issue stats = %d/%d, want 1/1", updated.DoneCount, updated.IssueCount)
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

// TestProjectResourceUpdateLifecycle covers the PUT endpoint added in MUL-2662:
// editing label / position / resource_ref independently must succeed, and a
// missing resource_type swap is enforced implicitly because the request body
// has no resource_type field.
func TestProjectResourceUpdateLifecycle(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Update lifecycle project",
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

	// Seed one local_directory resource we will mutate.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "local_directory",
		"resource_ref": map[string]any{
			"local_path": "/Users/foo/work/a",
			"daemon_id":  "d1",
			"label":      "A",
		},
		"label": "outer",
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectResource: %d %s", w.Code, w.Body.String())
	}
	var created ProjectResourceResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode CreateProjectResource: %v", err)
	}

	// Update only the label. Path/daemon/type must stay untouched, but the
	// ref's embedded label FOLLOWS the rename: older desktop builds read (and
	// write) only that copy, so leaving it behind would show them the previous
	// name forever (MUL-6323).
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID+"/resources/"+created.ID, map[string]any{
		"label": "renamed",
	})
	req = withURLParams(req, "id", project.ID, "resourceId", created.ID)
	testHandler.UpdateProjectResource(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProjectResource label-only: %d %s", w.Code, w.Body.String())
	}
	var updated ProjectResourceResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode UpdateProjectResource: %v", err)
	}
	if updated.Label == nil || *updated.Label != "renamed" {
		t.Errorf("after label edit: label = %v, want renamed", updated.Label)
	}
	var ref localDirectoryRef
	if err := json.Unmarshal(updated.ResourceRef, &ref); err != nil {
		t.Fatalf("decode resource_ref: %v", err)
	}
	if ref.LocalPath != "/Users/foo/work/a" || ref.DaemonID != "d1" {
		t.Errorf("label-only update leaked into resource_ref: %+v", ref)
	}
	if ref.Label != "renamed" {
		t.Errorf("ref label should mirror the rename, got %q", ref.Label)
	}

	// Update the ref payload (move to a new daemon path) and bump position.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID+"/resources/"+created.ID, map[string]any{
		"resource_ref": map[string]any{
			"local_path": "/Users/foo/work/b",
			"daemon_id":  "d2",
			"label":      "B",
		},
		"position": 5,
	})
	req = withURLParams(req, "id", project.ID, "resourceId", created.ID)
	testHandler.UpdateProjectResource(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProjectResource ref+position: %d %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode UpdateProjectResource: %v", err)
	}
	if err := json.Unmarshal(updated.ResourceRef, &ref); err != nil {
		t.Fatalf("decode resource_ref: %v", err)
	}
	if ref.LocalPath != "/Users/foo/work/b" || ref.DaemonID != "d2" {
		t.Errorf("ref-update mismatch: %+v", ref)
	}
	if updated.Position != 5 {
		t.Errorf("position = %d, want 5", updated.Position)
	}
	// This request changes the ref's label ("renamed" → "B") AND its path and
	// daemon, so it is not a rename — a client's ref is a snapshot, and only a
	// request whose sole difference is the label can be read as "the user
	// renamed this". The name therefore stays put in both homes, and callers
	// that mean to rename say so with the top-level label field.
	// TestProjectResourceStaleRefSnapshotDoesNotRollBackARename pins why:
	// otherwise an execution-mode dialog opened before someone else's rename
	// undoes it on save. TestProjectResourceLabelConvergence pins the matrix.
	if updated.Label == nil || *updated.Label != "renamed" {
		t.Errorf("an edit that is not a rename must not change the name, got %v", updated.Label)
	}
	if ref.Label != "renamed" {
		t.Errorf("ref copy should hold the row's current name, got %q", ref.Label)
	}

	// Explicit null clears the outer label — and the ref's copy with it, or
	// the UI would fall back to the very name the user deleted.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID+"/resources/"+created.ID, map[string]any{
		"label": nil,
	})
	req = withURLParams(req, "id", project.ID, "resourceId", created.ID)
	testHandler.UpdateProjectResource(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateProjectResource label=null: %d %s", w.Code, w.Body.String())
	}
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode UpdateProjectResource: %v", err)
	}
	if updated.Label != nil {
		t.Errorf("label should be cleared, got %v", *updated.Label)
	}
	var clearedFields map[string]json.RawMessage
	if err := json.Unmarshal(updated.ResourceRef, &clearedFields); err != nil {
		t.Fatalf("decode cleared resource_ref: %v", err)
	}
	if _, hasLabel := clearedFields["label"]; hasLabel {
		t.Errorf("clearing the label must also drop the ref copy, got %s", updated.ResourceRef)
	}

	// Bad ref payload must reject with 400 (relative path).
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID+"/resources/"+created.ID, map[string]any{
		"resource_ref": map[string]any{"local_path": "relative/path", "daemon_id": "d3"},
	})
	req = withURLParams(req, "id", project.ID, "resourceId", created.ID)
	testHandler.UpdateProjectResource(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("relative path: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Unknown resource id must 404.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID+"/resources/00000000-0000-0000-0000-000000000000", map[string]any{
		"label": "ghost",
	})
	req = withURLParams(req, "id", project.ID, "resourceId", "00000000-0000-0000-0000-000000000000")
	testHandler.UpdateProjectResource(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("missing resource: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestProjectResourceLabelConvergence pins how the server converges the two
// homes a local_directory display name historically has: the top-level label
// column (written by renames since they stopped resending the ref) and the
// legacy copy inside resource_ref (the only home desktop ≤ v0.4.28 knows,
// which those builds both read first and write renames into). The server is
// the only writer that sees both client generations, so every write must
// leave the two agreeing — otherwise each generation edits its own copy and
// the same row shows different names on different devices (MUL-6323).
func TestProjectResourceLabelConvergence(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Label convergence project",
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

	// Created the way current clients do: name inside the ref, no column.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "local_directory",
		"resource_ref": map[string]any{
			"local_path":     "/Users/dev/work/game-client",
			"daemon_id":      "daemon-label-convergence",
			"label":          "Game Client",
			"execution_mode": "in_place",
		},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectResource: %d %s", w.Code, w.Body.String())
	}
	var created ProjectResourceResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode CreateProjectResource: %v", err)
	}

	update := func(body map[string]any) ProjectResourceResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/projects/"+project.ID+"/resources/"+created.ID, body)
		req = withURLParams(req, "id", project.ID, "resourceId", created.ID)
		testHandler.UpdateProjectResource(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("UpdateProjectResource %v: %d %s", body, w.Code, w.Body.String())
		}
		var resp ProjectResourceResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode UpdateProjectResource: %v", err)
		}
		return resp
	}
	refFields := func(resp ProjectResourceResponse) map[string]json.RawMessage {
		t.Helper()
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(resp.ResourceRef, &fields); err != nil {
			t.Fatalf("decode resource_ref: %v", err)
		}
		return fields
	}
	refString := func(fields map[string]json.RawMessage, key string) string {
		t.Helper()
		rawValue, ok := fields[key]
		if !ok {
			return ""
		}
		var s string
		if err := json.Unmarshal(rawValue, &s); err != nil {
			t.Fatalf("ref[%q] not a string: %s", key, rawValue)
		}
		return s
	}

	// A rename from a desktop ≤ v0.4.28: the whole ref comes back with a new
	// embedded label and no label field. The column must follow, or clients
	// that read the column first keep showing the name this rename replaced.
	resp := update(map[string]any{
		"resource_ref": map[string]any{
			"local_path":     "/Users/dev/work/game-client",
			"daemon_id":      "daemon-label-convergence",
			"label":          "Renamed On Old Desktop",
			"execution_mode": "in_place",
		},
	})
	if resp.Label == nil || *resp.Label != "Renamed On Old Desktop" {
		t.Fatalf("column should follow a legacy ref rename, got %v", resp.Label)
	}

	// Plant what an even newer server may leave in the row: a ref field this
	// binary does not know. Label writes must patch around it, not re-marshal
	// it away — dropping unknown fields on an unrelated edit is the exact
	// failure this PR closes for execution_mode on old servers.
	futureRef := json.RawMessage(`{"local_path":"/Users/dev/work/game-client","daemon_id":"daemon-label-convergence","label":"Renamed On Old Desktop","execution_mode":"in_place","future_field":{"keep":"me"}}`)
	if _, err := testHandler.Queries.UpdateProjectResource(context.Background(), db.UpdateProjectResourceParams{
		ID:          parseUUID(created.ID),
		ResourceRef: futureRef,
		Label:       pgtype.Text{String: "Renamed On Old Desktop", Valid: true},
		Position:    0,
	}); err != nil {
		t.Fatalf("plant future ref: %v", err)
	}

	// A rename from a current client: label only, no ref. The ref's embedded
	// copy must follow so ≤ v0.4.28 readers see the new name — with the mode
	// and the unknown field untouched.
	resp = update(map[string]any{"label": "Renamed On New Desktop"})
	if resp.Label == nil || *resp.Label != "Renamed On New Desktop" {
		t.Fatalf("label-only rename: column = %v", resp.Label)
	}
	fields := refFields(resp)
	if got := refString(fields, "label"); got != "Renamed On New Desktop" {
		t.Errorf("ref label should mirror the rename for legacy readers, got %q", got)
	}
	if got := refString(fields, "execution_mode"); got != "in_place" {
		t.Errorf("rename must not touch execution_mode, got %q", got)
	}
	if string(fields["future_field"]) != `{"keep":"me"}` {
		t.Errorf("rename must preserve unknown ref fields and their values, got %s", fields["future_field"])
	}

	// A ref resent with its embedded label UNCHANGED — the execution-mode
	// dialog spreads the stored ref back — is not a rename. It must not
	// overwrite a column rename... but this write may converge the ref copy
	// onto the column. (The resend goes through ref normalization, so this is
	// also where future_field legitimately disappears on THIS server: that is
	// the documented normalize contract for provided refs, not label code.)
	if _, err := testHandler.Queries.UpdateProjectResource(context.Background(), db.UpdateProjectResourceParams{
		ID:          parseUUID(created.ID),
		ResourceRef: json.RawMessage(`{"local_path":"/Users/dev/work/game-client","daemon_id":"daemon-label-convergence","label":"Stale Ref Copy","execution_mode":"in_place"}`),
		Label:       pgtype.Text{String: "Column Rename", Valid: true},
		Position:    0,
	}); err != nil {
		t.Fatalf("plant diverged row: %v", err)
	}
	resp = update(map[string]any{
		"resource_ref": map[string]any{
			"local_path":     "/Users/dev/work/game-client",
			"daemon_id":      "daemon-label-convergence",
			"label":          "Stale Ref Copy",
			"execution_mode": "in_place",
		},
	})
	if resp.Label == nil || *resp.Label != "Column Rename" {
		t.Fatalf("unchanged ref label must not overwrite a column rename, got %v", resp.Label)
	}
	if got := refString(refFields(resp), "label"); got != "Column Rename" {
		t.Errorf("diverged ref copy should heal onto the column, got %q", got)
	}

	// Clearing the name removes BOTH copies: with either one left behind the
	// UI would resurrect the name the user just deleted.
	resp = update(map[string]any{"label": ""})
	if resp.Label != nil {
		t.Fatalf("clear: column = %v", *resp.Label)
	}
	fields = refFields(resp)
	if _, hasLabel := fields["label"]; hasLabel {
		t.Errorf("clear must drop the ref copy too, got %s", resp.ResourceRef)
	}
	if got := refString(fields, "execution_mode"); got != "in_place" {
		t.Errorf("clear must not touch execution_mode, got %q", got)
	}

	// A write that decides no label — position only — must leave the name of
	// a row whose only copy lives in the ref alone: NULL column here means
	// "never set", not "cleared".
	if _, err := testHandler.Queries.UpdateProjectResource(context.Background(), db.UpdateProjectResourceParams{
		ID:          parseUUID(created.ID),
		ResourceRef: json.RawMessage(`{"local_path":"/Users/dev/work/game-client","daemon_id":"daemon-label-convergence","label":"Ref Only Name","execution_mode":"in_place"}`),
		Label:       pgtype.Text{},
		Position:    0,
	}); err != nil {
		t.Fatalf("plant legacy row: %v", err)
	}
	resp = update(map[string]any{"position": 3})
	if got := refString(refFields(resp), "label"); got != "Ref Only Name" {
		t.Errorf("position-only update must not strip a ref-only name, got %q", got)
	}
	if resp.Label != nil {
		t.Errorf("position-only update invented a column label: %v", *resp.Label)
	}
}

// TestProjectResourceLocalDirectoryDaemonScopedConflict pins the project-level
// conflict check for local_directory: one row per daemon per project. The
// daemon-side resolver picks the first match by daemon_id, so silently
// allowing two rows on the same daemon — even at distinct paths — would let
// the agent write into whichever sorts first. The DB UNIQUE constraint only
// catches identical ref JSON; this check covers the broader invariant.
func TestProjectResourceLocalDirectoryDaemonScopedConflict(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Local dir daemon-scoped conflict",
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

	const (
		daemonID    = "d-scoped"
		otherDaemon = "d-other"
		localPath   = "/Users/foo/work/scoped"
	)

	// First attach succeeds.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "local_directory",
		"resource_ref": map[string]any{
			"local_path": localPath,
			"daemon_id":  daemonID,
			"label":      "first",
		},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("first attach: %d %s", w.Code, w.Body.String())
	}
	var first ProjectResourceResponse
	if err := json.NewDecoder(w.Body).Decode(&first); err != nil {
		t.Fatalf("decode first: %v", err)
	}

	// Same (daemon_id, local_path) with a different label must 409 — the
	// embedded label is human metadata, not a discriminator.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "local_directory",
		"resource_ref": map[string]any{
			"local_path": localPath,
			"daemon_id":  daemonID,
			"label":      "different label",
		},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("same daemon same path create: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// A second row on the same daemon at a DIFFERENT path must also 409 —
	// the daemon-scoped invariant rejects more than one local_directory
	// per (project, daemon), even if the paths differ.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "local_directory",
		"resource_ref": map[string]any{
			"local_path": "/Users/foo/work/other",
			"daemon_id":  daemonID,
			"label":      "other path",
		},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("same daemon different path create: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// Adding the same path on a DIFFERENT daemon is allowed — each daemon
	// gets to register exactly one local_directory.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "local_directory",
		"resource_ref": map[string]any{
			"local_path": localPath,
			"daemon_id":  otherDaemon,
			"label":      "other-machine",
		},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("other daemon attach: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var second ProjectResourceResponse
	if err := json.NewDecoder(w.Body).Decode(&second); err != nil {
		t.Fatalf("decode other-daemon row: %v", err)
	}

	// An UPDATE that drives the other-daemon row onto the first daemon must
	// also 409 — the first daemon already has a registration.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID+"/resources/"+second.ID, map[string]any{
		"resource_ref": map[string]any{
			"local_path": localPath,
			"daemon_id":  daemonID,
			"label":      "fresh",
		},
	})
	req = withURLParams(req, "id", project.ID, "resourceId", second.ID)
	testHandler.UpdateProjectResource(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("update onto existing daemon: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// Editing the same row in place (different label, same target) must
	// succeed — the conflict check ignores the row being updated.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID+"/resources/"+first.ID, map[string]any{
		"resource_ref": map[string]any{
			"local_path": localPath,
			"daemon_id":  daemonID,
			"label":      "renamed inline",
		},
	})
	req = withURLParams(req, "id", project.ID, "resourceId", first.ID)
	testHandler.UpdateProjectResource(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("in-place rename: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateProjectBundledLocalDirectoryDaemonConflict pins the second leg of
// the daemon-scoped invariant: a single POST /api/projects that bundles two
// local_directory resources on the same daemon — same path, same daemon
// with different labels, or different paths on the same daemon — must
// reject with 400 before any DB work.
func TestCreateProjectBundledLocalDirectoryDaemonConflict(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Bundled label shadow",
		"resources": []map[string]any{
			{
				"resource_type": "local_directory",
				"resource_ref": map[string]any{
					"local_path": "/Users/foo/work/dup",
					"daemon_id":  "d-bundle",
					"label":      "first",
				},
			},
			{
				"resource_type": "local_directory",
				"resource_ref": map[string]any{
					"local_path": "/Users/foo/work/dup",
					"daemon_id":  "d-bundle",
					"label":      "second label",
				},
			},
		},
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bundled label shadow: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// Confirm the rollback: no project with the title should exist.
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
		if p.Title == "Bundled label shadow" {
			t.Errorf("expected no project to survive bundled-create rejection, but found %s", p.ID)
		}
	}

	// Two distinct paths on the same daemon must ALSO 400 — the invariant
	// is "one local_directory per (project, daemon)", not "one per (project,
	// daemon, path)".
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Bundled distinct paths same daemon",
		"resources": []map[string]any{
			{
				"resource_type": "local_directory",
				"resource_ref": map[string]any{
					"local_path": "/Users/foo/work/a",
					"daemon_id":  "d-bundle",
					"label":      "A",
				},
			},
			{
				"resource_type": "local_directory",
				"resource_ref": map[string]any{
					"local_path": "/Users/foo/work/b",
					"daemon_id":  "d-bundle",
					"label":      "B",
				},
			},
		},
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("distinct-paths same daemon bundle: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	// A bundle with one row per daemon is allowed — each daemon owns its
	// own local_directory.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Bundled per-daemon rows",
		"resources": []map[string]any{
			{
				"resource_type": "local_directory",
				"resource_ref": map[string]any{
					"local_path": "/Users/foo/work/a",
					"daemon_id":  "d-bundle-1",
					"label":      "A",
				},
			},
			{
				"resource_type": "local_directory",
				"resource_ref": map[string]any{
					"local_path": "/Users/foo/work/b",
					"daemon_id":  "d-bundle-2",
					"label":      "B",
				},
			},
		},
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("per-daemon bundle: expected 201, got %d: %s", w.Code, w.Body.String())
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
	if len(resp.Resources) != 2 {
		t.Errorf("per-daemon bundle: expected 2 resources, got %d", len(resp.Resources))
	}
}

// execution_mode selects between the historical exclusive in-place run and
// worktree mode. It is validated at the API boundary so a typo is caught at
// save time; a daemon new enough to know the field refuses unknown values
// rather than falling back to in_place, so the two checks together keep a
// mistyped mode from ever running.
func TestValidateLocalDirectoryRefExecutionMode(t *testing.T) {
	accepted := []struct {
		name string
		mode string
		want string
	}{
		{"absent means in_place", "", ""},
		{"explicit in_place", "in_place", "in_place"},
		{"worktree", "worktree", "worktree"},
		{"surrounding whitespace is trimmed", "  worktree  ", "worktree"},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			ref := map[string]any{"local_path": "/Users/foo/work", "daemon_id": "d1"}
			if tc.mode != "" {
				ref["execution_mode"] = tc.mode
			}
			raw, err := json.Marshal(ref)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			out, err := validateLocalDirectoryRef(raw)
			if err != nil {
				t.Fatalf("validateLocalDirectoryRef: %v", err)
			}
			var got localDirectoryRef
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatalf("unmarshal normalized ref: %v", err)
			}
			if got.ExecutionMode != tc.want {
				t.Errorf("ExecutionMode = %q, want %q", got.ExecutionMode, tc.want)
			}
		})
	}

	rejected := []string{"snapshot", "WORKTREE", "in-place", "true"}
	for _, mode := range rejected {
		t.Run("rejects "+mode, func(t *testing.T) {
			raw, err := json.Marshal(map[string]any{
				"local_path":     "/Users/foo/work",
				"daemon_id":      "d1",
				"execution_mode": mode,
			})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if _, err := validateLocalDirectoryRef(raw); err == nil {
				t.Errorf("execution_mode %q was accepted, want a validation error", mode)
			}
		})
	}
}

// latestDaemonCLIVersion feeds the worktree-mode save gate: an old daemon
// json-skips execution_mode and would run tasks in place, so the gate has to
// read the version of the binary actually running on the machine — the
// freshest version-bearing row for that daemon_id, never a stale sibling row.
func TestLatestDaemonCLIVersion(t *testing.T) {
	row := func(daemonID, version string, seen time.Time) db.AgentRuntime {
		rt := db.AgentRuntime{
			DaemonID:   pgtype.Text{String: daemonID, Valid: daemonID != ""},
			LastSeenAt: pgtype.Timestamptz{Time: seen, Valid: !seen.IsZero()},
		}
		if version != "" {
			rt.Metadata = []byte(`{"cli_version":"` + version + `"}`)
		}
		return rt
	}
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		runtimes []db.AgentRuntime
		daemonID string
		want     string
	}{
		{
			name:     "no rows at all",
			runtimes: nil,
			daemonID: "d1",
			want:     "",
		},
		{
			name:     "only other daemons",
			runtimes: []db.AgentRuntime{row("d2", "0.9.9", base)},
			daemonID: "d1",
			want:     "",
		},
		{
			name:     "single match",
			runtimes: []db.AgentRuntime{row("d1", "0.4.24", base)},
			daemonID: "d1",
			want:     "0.4.24",
		},
		{
			name: "freshest row wins regardless of order",
			runtimes: []db.AgentRuntime{
				row("d1", "0.4.30", base.Add(2*time.Hour)),
				row("d1", "0.4.20", base),
			},
			daemonID: "d1",
			want:     "0.4.30",
		},
		{
			name: "stale-but-fresher row without a version does not mask an older versioned row",
			runtimes: []db.AgentRuntime{
				row("d1", "", base.Add(2*time.Hour)),
				row("d1", "0.4.24", base),
			},
			daemonID: "d1",
			want:     "0.4.24",
		},
		{
			name:     "match with no version anywhere",
			runtimes: []db.AgentRuntime{row("d1", "", base)},
			daemonID: "d1",
			want:     "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := latestDaemonCLIVersion(tc.runtimes, tc.daemonID); got != tc.want {
				t.Errorf("latestDaemonCLIVersion = %q, want %q", got, tc.want)
			}
		})
	}
}

// The bundled create path (project + resources in one transaction) skipped the
// worktree save gate that POST/PUT /resources have always run, so a project
// could be created with a mode the machine cannot run — surfacing only later,
// as the claim gate cancelling the task.
//
// It matters more now that the client no longer predicts this answer: the UI
// leaves parallel mode selectable and lets the server rule on it (#7113), which
// only works if every write path actually asks.
func TestCreateProjectGatesWorktreeLocalDirectory(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Bundled worktree resource",
		"resources": []map[string]any{
			{
				"resource_type": "local_directory",
				"resource_ref": map[string]any{
					"local_path":     "/Users/dev/work/game-client",
					"daemon_id":      "daemon-with-no-runtime-row",
					"execution_mode": "worktree",
				},
			},
		},
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("CreateProject with un-runnable worktree resource: expected 422, got %d: %s",
			w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["code"] != "daemon_version_unsupported" {
		t.Fatalf("expected the shared gate's error code, got %#v", resp["code"])
	}

	// The gate runs before the transaction opens, so the rejection must not
	// leave a half-created project behind.
	lw := httptest.NewRecorder()
	lreq := newRequest("GET", "/api/projects?workspace_id="+testWorkspaceID, nil)
	testHandler.ListProjects(lw, lreq)
	if strings.Contains(lw.Body.String(), "Bundled worktree resource") {
		t.Fatal("rejected create left a project behind")
	}
}

// createTestProjectForResources / createLocalDirectoryResourceFor build the
// fixtures the label-classification tests share, through the real handlers so
// the rows look exactly like a client's would.
func createTestProjectForResources(t *testing.T, title string) ProjectResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{"title": title})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		r := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		r = withURLParam(r, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	})
	return project
}

func createLocalDirectoryResourceFor(t *testing.T, projectID string, ref map[string]any) ProjectResourceResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/resources", map[string]any{
		"resource_type": "local_directory",
		"resource_ref":  ref,
	})
	req = withURLParam(req, "id", projectID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProjectResource: %d %s", w.Code, w.Body.String())
	}
	var created ProjectResourceResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode CreateProjectResource: %v", err)
	}
	return created
}

// A resource's execution-mode dialog snapshots the whole ref when it OPENS, and
// sends it back on save. If someone renames the folder from another device in
// between, that snapshot arrives carrying a label the row no longer has — and
// reading "the ref label differs" as "an old client renamed it" would undo the
// rename, from a dialog that has nothing to do with the name.
//
// Also pins the other half of the same classification: an old client's rename
// resends the ref but changes no execution semantics, so it must not be refused
// by the worktree capability gate for a machine whose registration has drifted.
func TestProjectResourceStaleRefSnapshotDoesNotRollBackARename(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	project := createTestProjectForResources(t, "Stale mode snapshot")
	const daemonID = "daemon-stale-snapshot"

	// The row as both devices last saw it: named "Game Client", in_place.
	created := createLocalDirectoryResourceFor(t, project.ID, map[string]any{
		"local_path": "/Users/dev/work/game-client",
		"daemon_id":  daemonID,
		"label":      "Game Client",
	})

	update := func(body map[string]any) ProjectResourceResponse {
		t.Helper()
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/projects/"+project.ID+"/resources/"+created.ID, body)
		req = withURLParams(req, "id", project.ID, "resourceId", created.ID)
		testHandler.UpdateProjectResource(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("UpdateProjectResource(%v): %d %s", body, w.Code, w.Body.String())
		}
		var resp ProjectResourceResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}
	refLabelOf := func(resp ProjectResourceResponse) string {
		t.Helper()
		var fields map[string]any
		if err := json.Unmarshal(resp.ResourceRef, &fields); err != nil {
			t.Fatalf("decode ref: %v", err)
		}
		label, _ := fields["label"].(string)
		return label
	}

	// Device B renames it. Both homes of the name now say "Renamed Client".
	renamed := update(map[string]any{"label": "Renamed Client"})
	if refLabelOf(renamed) != "Renamed Client" {
		t.Fatalf("setup: ref copy did not follow the rename: %s", renamed.ResourceRef)
	}

	// Device A, whose dialog opened before that, saves in_place → in_place with
	// its stale snapshot. The mode is what it came to change; the name is not.
	after := update(map[string]any{
		"resource_ref": map[string]any{
			"local_path":     "/Users/dev/work/game-client",
			"daemon_id":      daemonID,
			"label":          "Game Client",
			"execution_mode": "in_place",
		},
	})
	if after.Label == nil || *after.Label != "Renamed Client" {
		t.Fatalf("a stale mode snapshot rolled back the rename: column = %v", after.Label)
	}
	if got := refLabelOf(after); got != "Renamed Client" {
		t.Errorf("stale ref label should be healed to the current name, got %q", got)
	}

	// A genuine ≤ v0.4.28 rename — ref resent, ONLY the label different — still
	// counts, and still reaches the column.
	legacy := update(map[string]any{
		"resource_ref": map[string]any{
			"local_path":     "/Users/dev/work/game-client",
			"daemon_id":      daemonID,
			"label":          "Renamed On Old Desktop",
			"execution_mode": "in_place",
		},
	})
	if legacy.Label == nil || *legacy.Label != "Renamed On Old Desktop" {
		t.Fatalf("legacy rename should still reach the column, got %v", legacy.Label)
	}
}

// An old client renames by resending the ref, so the worktree capability gate
// would see "the ref was provided" and refuse the rename on a machine whose
// runtime registration no longer advertises the capability — failing an edit
// that changes nothing about what runs. The row already says worktree; the
// claim gate is what keeps it from running somewhere that cannot.
func TestProjectResourceLegacyRenameSkipsWorktreeGate(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	project := createTestProjectForResources(t, "Legacy rename on a worktree row")
	const daemonID = "daemon-no-runtime-row-for-rename"

	created := createLocalDirectoryResourceFor(t, project.ID, map[string]any{
		"local_path": "/Users/dev/work/game-client",
		"daemon_id":  daemonID,
		"label":      "Game Client",
	})
	// Plant the worktree mode directly: saving it through the API would (
	// correctly) be refused for a daemon with no capable runtime row.
	if _, err := testHandler.Queries.UpdateProjectResource(context.Background(), db.UpdateProjectResourceParams{
		ID:          parseUUID(created.ID),
		ResourceRef: json.RawMessage(`{"local_path":"/Users/dev/work/game-client","daemon_id":"` + daemonID + `","label":"Game Client","execution_mode":"worktree"}`),
		Label:       pgtype.Text{String: "Game Client", Valid: true},
		Position:    0,
	}); err != nil {
		t.Fatalf("plant worktree row: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/projects/"+project.ID+"/resources/"+created.ID, map[string]any{
		"resource_ref": map[string]any{
			"local_path":     "/Users/dev/work/game-client",
			"daemon_id":      daemonID,
			"label":          "Renamed On Old Desktop",
			"execution_mode": "worktree",
		},
	})
	req = withURLParams(req, "id", project.ID, "resourceId", created.ID)
	testHandler.UpdateProjectResource(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("a rename that changes no execution semantics must not hit the capability gate: %d %s",
			w.Code, w.Body.String())
	}

	// And the gate still fires when the same client actually changes the mode.
	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID+"/resources/"+created.ID, map[string]any{
		"resource_ref": map[string]any{
			"local_path":     "/Users/dev/work/game-client",
			"daemon_id":      daemonID,
			"label":          "Renamed On Old Desktop",
			"execution_mode": "in_place",
		},
	})
	req = withURLParams(req, "id", project.ID, "resourceId", created.ID)
	testHandler.UpdateProjectResource(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("switching to in_place needs no capability: %d %s", w.Code, w.Body.String())
	}
}
