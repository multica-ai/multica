package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// MUL-9284: the workspace owner can list every private project and let
// themselves into one; nobody else can, and listing opens no content.

func TestWorkspaceOwnerListsPrivateProjects(t *testing.T) {
	f := newPrivateProjectFixture(t)
	removeProjectMemberRow(t, f.projectID, testUserID)
	joined := dbfx.Project(t, f.marker+" joined project", testutil.Cols{"visibility": projectaccess.VisibilityPrivate})
	addProjectMemberRow(t, joined, testUserID)
	open := dbfx.Project(t, f.marker+" open project")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = $1`, joined)
	})

	list := func(req *http.Request) *testutil.Response {
		return testutil.Call(t, testHandler.ListPrivateProjectsForOwner, req)
	}
	get := func(userID string) *http.Request {
		return asUser(newRequest(http.MethodGet, "/api/projects/private", nil), userID)
	}

	// Owner only: an admin's role is not enough, and neither is membership.
	list(get(f.outsideAdmin)).Want(http.StatusForbidden)
	list(get(f.insider)).Want(http.StatusForbidden)
	list(get(f.outsider)).Want(http.StatusForbidden)

	// Nor an agent, even one running for the owner.
	agent := dbfx.Agent(t, f.marker+" owner agent", testRuntimeID, testutil.Cols{"owner_id": testUserID})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": f.publicIssue, "status": "running",
		"originator_user_id": testUserID, "accountable_user_id": testUserID})
	asAgent := get(testUserID)
	asAgent.Header.Set("X-Actor-Source", "task_token")
	asAgent.Header.Set("X-Agent-ID", agent)
	asAgent.Header.Set("X-Task-ID", task)
	list(asAgent).Want(http.StatusForbidden)

	// Nor a cloud node PAT, which auth stamps with its owner's user id.
	asCloudPAT := get(testUserID)
	asCloudPAT.Header.Set("X-Actor-Source", "cloud_pat")
	list(asCloudPAT).Want(http.StatusForbidden)

	var resp struct {
		Projects []PrivateProjectOverview `json:"projects"`
	}
	list(get(testUserID)).Want(http.StatusOK).JSON(&resp)
	byID := map[string]PrivateProjectOverview{}
	for _, p := range resp.Projects {
		byID[p.ID] = p
	}
	if _, ok := byID[open]; ok {
		t.Error("a workspace-visible project is listed as private")
	}
	secret, ok := byID[f.projectID]
	if !ok {
		t.Fatal("the private project the owner is not in is missing: it is the one this list exists for")
	}
	if secret.IsMember || secret.Title != f.marker+" secret project" {
		t.Errorf("unjoined project = %+v, want its title and is_member=false", secret)
	}
	if len(secret.Members) != 1 || secret.Members[0].UserID != f.insider || secret.Members[0].Email == "" {
		t.Errorf("unjoined project members = %+v, want the one insider with their identity", secret.Members)
	}
	if got, ok := byID[joined]; !ok || !got.IsMember {
		t.Errorf("joined project = %+v (listed %v), want is_member=true", got, ok)
	}

	// The list reveals names and members only: the content stays closed.
	req := withURLParam(asUser(newRequest(http.MethodGet, "/api/issues/"+f.secretIssue, nil), testUserID), "id", f.secretIssue)
	testutil.Call(t, testHandler.GetIssue, req).Want(http.StatusNotFound)
}

func TestWorkspaceOwnerJoinsPrivateProject(t *testing.T) {
	ctx := context.Background()
	f := newPrivateProjectFixture(t)
	removeProjectMemberRow(t, f.projectID, testUserID)
	dbfx.Exec(t, `UPDATE project SET lead_type = 'member', lead_id = $1 WHERE id = $2`, f.insider, f.projectID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = $2`, testWorkspaceID, inboxTypeProjectOwnerJoined)
	})

	join := func(userID, projectID string) *testutil.Response {
		req := withURLParam(asUser(newRequest(http.MethodPost, "/api/projects/"+projectID+"/join", nil), userID), "id", projectID)
		return testutil.Call(t, testHandler.JoinPrivateProject, req)
	}
	getIssue := func(userID string) *testutil.Response {
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/issues/"+f.secretIssue, nil), userID), "id", f.secretIssue)
		return testutil.Call(t, testHandler.GetIssue, req)
	}
	notices := func(recipient string) int {
		return dbfx.Count(t, `SELECT count(*) FROM inbox_item
			WHERE workspace_id = $1 AND recipient_id = $2 AND type = $3 AND details->>'project_id' = $4`,
			testWorkspaceID, recipient, inboxTypeProjectOwnerJoined, f.projectID)
	}

	// Only the owner has this way in.
	join(f.outsideAdmin, f.projectID).Want(http.StatusForbidden)
	join(f.outsider, f.projectID).Want(http.StatusForbidden)
	// Machine credentials carry the owner's user id but not their way in.
	for _, source := range []string{"task_token", "cloud_pat"} {
		req := withURLParam(asUser(newRequest(http.MethodPost, "/api/projects/"+f.projectID+"/join", nil), testUserID), "id", f.projectID)
		req.Header.Set("X-Actor-Source", source)
		testutil.Call(t, testHandler.JoinPrivateProject, req).Want(http.StatusForbidden)
	}
	if members := projectMemberIDs(t, f.projectID); members[f.outsideAdmin] || members[f.outsider] || members[testUserID] {
		t.Fatalf("a refused join still added a member: %v", members)
	}
	join(testUserID, "00000000-0000-0000-0000-0000000000ff").Want(http.StatusNotFound)
	open := dbfx.Project(t, f.marker+" open project")
	join(testUserID, open).Want(http.StatusBadRequest)

	getIssue(testUserID).Want(http.StatusNotFound)
	join(testUserID, f.projectID).Want(http.StatusOK)
	getIssue(testUserID).Want(http.StatusOK)

	if members := projectMemberIDs(t, f.projectID); !members[testUserID] || !members[f.insider] {
		t.Fatalf("members after join = %v, want the owner listed beside the insider", members)
	}
	// The project had a lead who is still a member: joining does not take it.
	if got := loadProjectLead(t, f.projectID); got != (projectLead{"member", f.insider}) {
		t.Errorf("lead after join = %+v, want the existing lead", got)
	}
	// The members who were already there are told; the owner is not.
	if n := notices(f.insider); n != 1 {
		t.Errorf("insider notices = %d, want 1", n)
	}
	if n := notices(testUserID); n != 0 {
		t.Errorf("owner notices = %d, want 0", n)
	}
	var actor, title string
	if err := testPool.QueryRow(ctx, `SELECT actor_id::text, title FROM inbox_item WHERE recipient_id = $1 AND type = $2 AND details->>'project_id' = $3`,
		f.insider, inboxTypeProjectOwnerJoined, f.projectID).Scan(&actor, &title); err != nil {
		t.Fatal(err)
	}
	if actor != testUserID || title != f.marker+" secret project" {
		t.Errorf("notice actor = %s title = %q, want the owner and the project title", actor, title)
	}

	// Joining again is a no-op: no second notice.
	join(testUserID, f.projectID).Want(http.StatusOK)
	if n := notices(f.insider); n != 1 {
		t.Errorf("insider notices after a repeated join = %d, want 1", n)
	}

	// Joining never touches the lead. On a project nobody is left in, the
	// owner manages access through their role once they are on the list.
	t.Run("an abandoned project keeps its lead", func(t *testing.T) {
		agentID := dbfx.Agent(t, f.marker+" lead agent", testRuntimeID)
		abandoned := dbfx.Project(t, f.marker+" abandoned", testutil.Cols{
			"visibility": projectaccess.VisibilityPrivate, "lead_type": "agent", "lead_id": agentID,
		})
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = $1`, abandoned)
		})
		join(testUserID, abandoned).Want(http.StatusOK)
		if got := loadProjectLead(t, abandoned); got != (projectLead{"agent", agentID}) {
			t.Errorf("lead = %+v, want the agent it had", got)
		}
		if got := projectManagerID(t, testUserID, abandoned); got != testUserID {
			t.Errorf("manager_id = %q, want the owner, its only member", got)
		}
		addProjectMembersAs(t, testUserID, abandoned, f.outsider).Want(http.StatusOK)
	})

	t.Run("a lead outside the member list stays", func(t *testing.T) {
		stale := dbfx.Project(t, f.marker+" stale lead", testutil.Cols{
			"visibility": projectaccess.VisibilityPrivate, "lead_type": "member", "lead_id": f.outsider,
		})
		addProjectMemberRow(t, stale, f.insider)
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = $1`, stale)
		})
		join(testUserID, stale).Want(http.StatusOK)
		if got := loadProjectLead(t, stale); got != (projectLead{"member", f.outsider}) {
			t.Errorf("lead = %+v, want it left as it was", got)
		}
		// The member who was already there still manages it, beside the owner.
		if got := projectManagerID(t, testUserID, stale); got != f.insider {
			t.Errorf("manager_id = %q, want the member who was there first", got)
		}
	})
}
