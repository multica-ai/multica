package handler

// CEREBRO-PATCH(project-access-handler): cerebro modification of upstream file

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// EVAL for Phase 4 — project membership endpoints. Verifies admin-gated
// access toggle and member CRUD. Non-admins must be rejected.

func TestUpdateProjectAccess_AdminCanFlip(t *testing.T) {
	f := setupFilterFixture(t)

	// owner makes the call
	owner, err := testHandler.Queries.GetMemberByUserAndWorkspace(
		newReqAs("GET", "/", testUserID, db.Member{}).Context(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      util.ParseUUID(testUserID),
			WorkspaceID: util.ParseUUID(testWorkspaceID),
		})
	if err != nil {
		t.Fatalf("load owner: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"access": "workspace"})
	req := httptest.NewRequest("PATCH", "/api/projects/"+f.restrictedID+"/access", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, owner))
	req = withURLParam(req, "id", f.restrictedID)

	rec := httptest.NewRecorder()
	testHandler.UpdateProjectAccess(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin flip: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateProjectAccess_MemberRejected(t *testing.T) {
	f := setupFilterFixture(t)

	body, _ := json.Marshal(map[string]string{"access": "workspace"})
	req := httptest.NewRequest("PATCH", "/api/projects/"+f.restrictedID+"/access", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", f.memberUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, f.memberMember))
	req = withURLParam(req, "id", f.restrictedID)

	rec := httptest.NewRecorder()
	testHandler.UpdateProjectAccess(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin flip: want 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAddProjectMember_AdminCanAdd(t *testing.T) {
	f := setupFilterFixture(t)

	owner, _ := testHandler.Queries.GetMemberByUserAndWorkspace(
		newReqAs("GET", "/", testUserID, db.Member{}).Context(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      util.ParseUUID(testUserID),
			WorkspaceID: util.ParseUUID(testWorkspaceID),
		})

	body, _ := json.Marshal(map[string]string{"user_id": f.outsideUserID})
	req := httptest.NewRequest("POST", "/api/projects/"+f.restrictedID+"/members", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, owner))
	req = withURLParam(req, "id", f.restrictedID)

	rec := httptest.NewRecorder()
	testHandler.AddProjectMember(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("add project member as admin: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Verify the now-added user can see the project's restricted issue
	got, err := testHandler.Queries.IsProjectMember(req.Context(), db.IsProjectMemberParams{
		ProjectID: util.ParseUUID(f.restrictedID),
		UserID:    util.ParseUUID(f.outsideUserID),
	})
	if err != nil || !got {
		t.Fatalf("expected outside user to be project member after add (err=%v ok=%v)", err, got)
	}
}

func TestRemoveProjectMember_AdminCanRemove(t *testing.T) {
	f := setupFilterFixture(t)

	owner, _ := testHandler.Queries.GetMemberByUserAndWorkspace(
		newReqAs("GET", "/", testUserID, db.Member{}).Context(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      util.ParseUUID(testUserID),
			WorkspaceID: util.ParseUUID(testWorkspaceID),
		})

	req := httptest.NewRequest("DELETE", "/api/projects/"+f.restrictedID+"/members/"+f.memberUserID, nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, owner))
	req = withURLParams(req, "id", f.restrictedID, "userId", f.memberUserID)

	rec := httptest.NewRecorder()
	testHandler.RemoveProjectMember(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("remove: want 204, got %d body=%s", rec.Code, rec.Body.String())
	}

	got, _ := testHandler.Queries.IsProjectMember(req.Context(), db.IsProjectMemberParams{
		ProjectID: util.ParseUUID(f.restrictedID),
		UserID:    util.ParseUUID(f.memberUserID),
	})
	if got {
		t.Fatalf("user should no longer be project member after remove")
	}
}

// EVAL for Phase 7 — workspace member detail Projects tab. Admin requesting
// /workspaces/{id}/members/{memberId}/projects sees only restricted projects
// the member belongs to. Non-admins get 403.
func TestListMemberProjects_AdminSeesRestrictedOnly(t *testing.T) {
	f := setupFilterFixture(t)
	ctx := newReqAs("GET", "/", testUserID, db.Member{}).Context()

	owner, _ := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.ParseUUID(testUserID),
		WorkspaceID: util.ParseUUID(testWorkspaceID),
	})

	// Look up the *member* row id (not user_id) for the "member" user.
	targetMember, err := testHandler.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.ParseUUID(f.memberUserID),
		WorkspaceID: util.ParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member row: %v", err)
	}
	memberID := util.UUIDToString(targetMember.ID)

	req := httptest.NewRequest("GET", "/api/workspaces/"+testWorkspaceID+"/members/"+memberID+"/projects", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, owner))
	req = withURLParams(req, "id", testWorkspaceID, "memberId", memberID)

	rec := httptest.NewRecorder()
	testHandler.ListMemberProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin list member projects: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Projects []map[string]any `json:"projects"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	// Member belongs to f.restrictedID — should appear; f.openProjectID is
	// not in project_member rows (it's open) so should not appear.
	hasRestricted := false
	hasOpen := false
	for _, p := range body.Projects {
		if id, _ := p["id"].(string); id == f.restrictedID {
			hasRestricted = true
		}
		if id, _ := p["id"].(string); id == f.openProjectID {
			hasOpen = true
		}
	}
	if !hasRestricted {
		t.Fatalf("expected member's restricted project in result, got %v", body.Projects)
	}
	if hasOpen {
		t.Fatalf("open project should NOT appear (only restricted ones)")
	}
}

// Regression: ProjectResponse must include the `access` field so the UI
// can render the Restricted dot and the members panel. Missing this field
// silently broke the project-access tab in production: project.access was
// undefined on the client → the members panel never appeared after flipping
// to restricted, and dots never showed in the projects list.
func TestProjectResponse_IncludesAccess(t *testing.T) {
	f := setupFilterFixture(t)

	owner, _ := testHandler.Queries.GetMemberByUserAndWorkspace(
		newReqAs("GET", "/", testUserID, db.Member{}).Context(),
		db.GetMemberByUserAndWorkspaceParams{
			UserID:      util.ParseUUID(testUserID),
			WorkspaceID: util.ParseUUID(testWorkspaceID),
		})

	req := newReqAs("GET", "/api/projects", testUserID, owner)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req = req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, owner))
	rec := httptest.NewRecorder()
	testHandler.ListProjects(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list projects: want 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Projects []map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Projects) == 0 {
		t.Fatalf("expected at least one project in response")
	}
	for _, p := range body.Projects {
		access, ok := p["access"].(string)
		if !ok {
			t.Fatalf("project missing access field, got keys=%v", keys(p))
		}
		if access != "workspace" && access != "restricted" {
			t.Fatalf("invalid access value: %q", access)
		}
		if id, _ := p["id"].(string); id == f.restrictedID && access != "restricted" {
			t.Fatalf("restricted project's access should be 'restricted', got %q", access)
		}
	}

	// Single-project GET path also needs it.
	req2 := newReqAs("GET", "/api/projects/"+f.restrictedID, testUserID, owner)
	req2.Header.Set("X-Workspace-ID", testWorkspaceID)
	req2 = req2.WithContext(middleware.SetMemberContext(req2.Context(), testWorkspaceID, owner))
	req2 = withURLParam(req2, "id", f.restrictedID)
	rec2 := httptest.NewRecorder()
	testHandler.GetProject(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("get project: want 200, got %d body=%s", rec2.Code, rec2.Body.String())
	}
	var single map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &single)
	if single["access"] != "restricted" {
		t.Fatalf("GET project: access=%v, want restricted", single["access"])
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestListProjectMembers_RestrictedHiddenFromOutsider(t *testing.T) {
	f := setupFilterFixture(t)

	req := newReqAs("GET", "/api/projects/"+f.restrictedID+"/members", f.outsideUserID, f.outsideMember)
	req = withURLParam(req, "id", f.restrictedID)

	rec := httptest.NewRecorder()
	testHandler.ListProjectMembers(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("outsider listing restricted-project members: want 404, got %d", rec.Code)
	}
}
