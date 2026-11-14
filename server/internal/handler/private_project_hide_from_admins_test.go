package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func createWorkspaceAdmin(t *testing.T, email string) string {
	t.Helper()
	userID := createPlainMember(t, email)
	dbfx.Exec(t, `UPDATE member SET role = 'admin' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
	return userID
}

func TestPrivateProjectHideFromAdmins(t *testing.T) {
	f := newPrivateProjectFixture(t)
	admin := createWorkspaceAdmin(t, f.marker+"-admin@multica.test")

	getIssue := func(userID string) int {
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/issues/"+f.secretIssue, nil), userID), "id", f.secretIssue)
		rec := testutil.Call(t, testHandler.GetIssue, req)
		return rec.Code
	}
	update := func(userID string, body map[string]any) *testutil.Response {
		req := withURLParam(asUser(newRequest(http.MethodPut, "/api/projects/"+f.projectID, body), userID), "id", f.projectID)
		return testutil.Call(t, testHandler.UpdateProject, req)
	}

	if got := getIssue(admin); got != http.StatusOK {
		t.Fatalf("admin before hide_from_admins: GET = %d, want 200", got)
	}

	// Only an access manager may switch it.
	update(f.insider, map[string]any{"hide_from_admins": true}).Want(http.StatusForbidden)
	var updated ProjectResponse
	update(testUserID, map[string]any{"hide_from_admins": true}).Want(http.StatusOK).JSON(&updated)
	if !updated.HideFromAdmins {
		t.Fatal("hide_from_admins not set in the response")
	}

	for _, tc := range []struct {
		name   string
		userID string
		want   int
	}{
		{"admin outside the member list", admin, http.StatusNotFound},
		{"workspace owner outside the member list", testUserID, http.StatusOK},
		{"project member", f.insider, http.StatusOK},
		{"regular member outside the project", f.outsider, http.StatusNotFound},
	} {
		if got := getIssue(tc.userID); got != tc.want {
			t.Errorf("%s: GET secret issue = %d, want %d", tc.name, got, tc.want)
		}
	}

	var list struct {
		Issues []IssueResponse `json:"issues"`
	}
	testutil.Call(t, testHandler.ListIssues, asUser(newRequest(http.MethodGet, "/api/issues?limit=100", nil), admin)).Want(http.StatusOK).JSON(&list)
	for _, is := range list.Issues {
		if is.ID == f.secretIssue {
			t.Fatal("hidden admin still lists the secret issue")
		}
	}

	// A hidden admin cannot reach the project to undo the setting or add
	// themselves back.
	update(admin, map[string]any{"hide_from_admins": false}).Want(http.StatusNotFound)
	addReq := withURLParam(asUser(newRequest(http.MethodPost, "/api/projects/"+f.projectID+"/members", map[string]any{"user_ids": []string{admin}}), admin), "id", f.projectID)
	testutil.Call(t, testHandler.AddProjectMembers, addReq).Want(http.StatusNotFound)

	// Notifications about the project skip the hidden admin too.
	_, err := testHandler.Queries.CreateInboxItem(context.Background(), db.CreateInboxItemParams{
		WorkspaceID:   parseUUID(testWorkspaceID),
		RecipientType: "member",
		RecipientID:   parseUUID(admin),
		Type:          "mentioned",
		Severity:      "info",
		IssueID:       parseUUID(f.secretIssue),
		Title:         f.marker + " secret notification",
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, f.secretIssue)
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("hidden admin notification: err = %v, want skipped", err)
	}

	// An admin the owner adds as a project member sees it again.
	addProjectMemberRow(t, f.projectID, admin)
	if got := getIssue(admin); got != http.StatusOK {
		t.Fatalf("admin added as project member: GET = %d, want 200", got)
	}

	update(testUserID, map[string]any{"hide_from_admins": false}).Want(http.StatusOK).JSON(&updated)
	if updated.HideFromAdmins {
		t.Fatal("hide_from_admins still set after clearing it")
	}
}

func TestHideFromAdminsNeedsPrivateProject(t *testing.T) {
	f := newPrivateProjectFixture(t)
	update := func(body map[string]any) *testutil.Response {
		req := withURLParam(asUser(newRequest(http.MethodPut, "/api/projects/"+f.projectID, body), testUserID), "id", f.projectID)
		return testutil.Call(t, testHandler.UpdateProject, req)
	}
	update(map[string]any{"visibility": "workspace", "hide_from_admins": true}).Want(http.StatusBadRequest)
	var project ProjectResponse
	update(map[string]any{"visibility": "workspace"}).Want(http.StatusOK).JSON(&project)
	update(map[string]any{"hide_from_admins": true}).Want(http.StatusBadRequest)
	// Making it private and hiding it from admins in one request works.
	update(map[string]any{"visibility": "private", "hide_from_admins": true}).Want(http.StatusOK).JSON(&project)
	if project.Visibility != "private" || !project.HideFromAdmins {
		t.Fatalf("visibility = %q, hide_from_admins = %v", project.Visibility, project.HideFromAdmins)
	}
}

func TestBecomingPrivateHidesFromAdminsByDefault(t *testing.T) {
	f := newPrivateProjectFixture(t)
	create := func(body map[string]any) ProjectResponse {
		body["title"] = f.marker + " new"
		var p ProjectResponse
		testutil.Call(t, testHandler.CreateProject, asUser(newRequest(http.MethodPost, "/api/projects", body), testUserID)).Want(http.StatusCreated).JSON(&p)
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = $1`, p.ID)
			testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, p.ID)
		})
		return p
	}
	if p := create(map[string]any{"visibility": "private"}); !p.HideFromAdmins {
		t.Error("a project created private must hide from admins unless asked not to")
	}
	if p := create(map[string]any{"visibility": "private", "hide_from_admins": false}); p.HideFromAdmins {
		t.Error("hide_from_admins=false on create was ignored")
	}
	if p := create(map[string]any{}); p.HideFromAdmins {
		t.Error("a workspace-visible project must not hide from admins")
	}
	testutil.Call(t, testHandler.CreateProject, asUser(newRequest(http.MethodPost, "/api/projects", map[string]any{"title": f.marker + " bad", "hide_from_admins": true}), testUserID)).Want(http.StatusBadRequest)

	update := func(id string, body map[string]any) ProjectResponse {
		var p ProjectResponse
		req := withURLParam(asUser(newRequest(http.MethodPut, "/api/projects/"+id, body), testUserID), "id", id)
		testutil.Call(t, testHandler.UpdateProject, req).Want(http.StatusOK).JSON(&p)
		return p
	}
	open := create(map[string]any{})
	if p := update(open.ID, map[string]any{"visibility": "private"}); !p.HideFromAdmins {
		t.Error("switching to private must hide from admins unless asked not to")
	}
	update(open.ID, map[string]any{"visibility": "workspace"})
	if p := update(open.ID, map[string]any{"visibility": "private", "hide_from_admins": false}); p.HideFromAdmins {
		t.Error("hide_from_admins=false with a switch to private was ignored")
	}
	// An edit that leaves visibility alone never changes the flag.
	if p := update(open.ID, map[string]any{"title": f.marker + " renamed"}); p.HideFromAdmins {
		t.Error("an unrelated edit turned hide_from_admins on")
	}
}

func TestAdminHidingProjectFromAdminsKeepsAccess(t *testing.T) {
	f := newPrivateProjectFixture(t)
	admin := createWorkspaceAdmin(t, f.marker+"-admin@multica.test")
	req := withURLParam(asUser(newRequest(http.MethodPut, "/api/projects/"+f.projectID, map[string]any{"hide_from_admins": true}), admin), "id", f.projectID)
	testutil.Call(t, testHandler.UpdateProject, req).Want(http.StatusOK)
	get := withURLParam(asUser(newRequest(http.MethodGet, "/api/issues/"+f.secretIssue, nil), admin), "id", f.secretIssue)
	testutil.Call(t, testHandler.GetIssue, get).Want(http.StatusOK)
}
