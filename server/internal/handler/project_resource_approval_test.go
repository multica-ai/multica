package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// helper: create an empty project for the test, returning its ID. The caller
// is responsible for deferring deletion. Mirrors the inline pattern used by
// TestProjectResourceLifecycle but lifted out so both new tests stay short.
func createBareProject(t *testing.T, title string) string {
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
	if err := json.NewDecoder(w.Body).Decode(&p); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		r := newRequest("DELETE", "/api/projects/"+p.ID, nil)
		r = withURLParam(r, "id", p.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), r)
	})
	return p.ID
}

// withApprovalFlag temporarily sets testHandler.cfg.RepoApprovalRequired and
// restores it after the test. Tests run sequentially within the package so
// mutating shared handler state is safe; t.Cleanup restores it even on panic.
func withApprovalFlag(t *testing.T, value bool) {
	t.Helper()
	prev := testHandler.cfg.RepoApprovalRequired
	testHandler.cfg.RepoApprovalRequired = value
	t.Cleanup(func() { testHandler.cfg.RepoApprovalRequired = prev })
}

func TestCreateProjectResource_AllowsArbitraryURL_WhenApprovalOff(t *testing.T) {
	withApprovalFlag(t, false)
	projectID := createBareProject(t, "approval-off project")

	// URL is intentionally NOT in workspace.repos; with the flag off, the
	// approved-set check is skipped and only URL/domain validation runs.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/resources", map[string]any{
		"resource_type": "github_repo",
		"resource_ref":  map[string]any{"url": "https://github.com/multica-ai/some-other-repo"},
	})
	req = withURLParam(req, "id", projectID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateProjectResource_RejectsUnapproved_WhenApprovalOn(t *testing.T) {
	withApprovalFlag(t, true)
	projectID := createBareProject(t, "approval-on project")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects/"+projectID+"/resources", map[string]any{
		"resource_type": "github_repo",
		"resource_ref":  map[string]any{"url": "https://github.com/multica-ai/some-other-repo"},
	})
	req = withURLParam(req, "id", projectID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "approved list") {
		t.Errorf("expected error mentioning approved list, got %s", w.Body.String())
	}
}
