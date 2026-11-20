package handler

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// guestFixture: a guest of one workspace-visible project (shared), with a
// second workspace-visible project (other), a private project (secret) and an
// issue in each plus one issue outside any project.
type guestFixture struct {
	guest       string
	shared      string
	other       string
	secret      string
	sharedIssue string
	otherIssue  string
	secretIssue string
	looseIssue  string
	guestMember string
	marker      string
}

func newGuestFixture(t *testing.T) guestFixture {
	t.Helper()
	marker := fmt.Sprintf("guest%d", time.Now().UnixNano())
	f := guestFixture{marker: marker}
	f.guest = createPlainMember(t, marker+"@multica.test")
	dbfx.Exec(t, `UPDATE member SET role = 'guest' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, f.guest)
	dbfx.QueryRow(t, `SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, f.guest).Scan(&f.guestMember)
	f.shared = dbfx.Project(t, marker+" shared")
	f.other = dbfx.Project(t, marker+" other")
	f.secret = dbfx.Project(t, marker+" secret", testutil.Cols{"visibility": projectaccess.VisibilityPrivate})
	addProjectMemberRow(t, f.shared, f.guest)
	addProjectMemberRow(t, f.secret, testUserID)
	base := 700000 + int(time.Now().UnixNano()%100000)*4
	f.sharedIssue = dbfx.Issue(t, marker+" shared issue", testutil.Cols{"project_id": f.shared, "number": base})
	f.otherIssue = dbfx.Issue(t, marker+" other issue", testutil.Cols{"project_id": f.other, "number": base + 1})
	f.secretIssue = dbfx.Issue(t, marker+" secret issue", testutil.Cols{"project_id": f.secret, "number": base + 2})
	f.looseIssue = dbfx.Issue(t, marker+" loose issue", testutil.Cols{"number": base + 3})
	return f
}

// guestRequest runs handler behind the workspace middleware, as the router
// does, so the guest route allowlist applies. It speaks as a guest-aware
// client: only those can sign a guest in.
func guestRequest(t *testing.T, handler http.HandlerFunc, userID, method, path string, body any, params ...string) *testutil.Response {
	t.Helper()
	req := withGuestCapability(asUser(newRequest(method, path, body), userID))
	if len(params) > 0 {
		req = withURLParams(req, params...)
	}
	return testutil.Call(t, inboxWorkspaceHandler(handler), req)
}

func TestGuestSeesOnlyItsProjects(t *testing.T) {
	f := newGuestFixture(t)

	for _, tc := range []struct {
		name  string
		issue string
		want  int
	}{
		{"issue in its project", f.sharedIssue, http.StatusOK},
		{"issue in another workspace project", f.otherIssue, http.StatusNotFound},
		{"issue in a private project", f.secretIssue, http.StatusNotFound},
		{"issue outside any project", f.looseIssue, http.StatusNotFound},
	} {
		rec := guestRequest(t, testHandler.GetIssue, f.guest, http.MethodGet, "/api/issues/"+tc.issue, nil, "id", tc.issue)
		if rec.Code != tc.want {
			t.Errorf("%s: GET = %d, want %d", tc.name, rec.Code, tc.want)
		}
	}

	var issues struct {
		Issues []IssueResponse `json:"issues"`
	}
	rec := guestRequest(t, testHandler.ListIssues, f.guest, http.MethodGet, "/api/issues?limit=200", nil)
	rec.Want(http.StatusOK).JSON(&issues)
	for _, is := range issues.Issues {
		if is.ProjectID == nil || *is.ProjectID != f.shared {
			t.Errorf("guest list contains issue %s outside its project", is.ID)
		}
	}
	if len(issues.Issues) == 0 {
		t.Error("guest list is empty, want the shared issue")
	}

	var projects struct {
		Projects []ProjectResponse `json:"projects"`
	}
	rec = guestRequest(t, testHandler.ListProjects, f.guest, http.MethodGet, "/api/projects", nil)
	rec.Want(http.StatusOK).JSON(&projects)
	if len(projects.Projects) != 1 || projects.Projects[0].ID != f.shared {
		ids := []string{}
		for _, p := range projects.Projects {
			ids = append(ids, p.ID)
		}
		t.Errorf("guest projects = %v, want only %s", ids, f.shared)
	}

	// A regular member still sees the workspace project and the loose issue.
	member := createPlainMember(t, f.marker+"-member@multica.test")
	rec = guestRequest(t, testHandler.GetIssue, member, http.MethodGet, "/api/issues/"+f.looseIssue, nil, "id", f.looseIssue)
	if rec.Code != http.StatusOK {
		t.Errorf("member GET loose issue = %d, want 200", rec.Code)
	}
}

func TestGuestRoutesAreDenyByDefault(t *testing.T) {
	f := newGuestFixture(t)
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/issues/" + f.sharedIssue, http.StatusNoContent},
		{http.MethodPost, "/api/issues/" + f.sharedIssue + "/comments", http.StatusNoContent},
		{http.MethodGet, "/api/projects", http.StatusNoContent},
		{http.MethodPut, "/api/issues/" + f.sharedIssue, http.StatusForbidden},
		{http.MethodPost, "/api/issues", http.StatusForbidden},
		{http.MethodPost, "/api/projects", http.StatusForbidden},
		{http.MethodGet, "/api/autopilots", http.StatusForbidden},
		{http.MethodGet, "/api/runtimes/" + f.sharedIssue, http.StatusForbidden},
		{http.MethodPost, "/api/chat/sessions", http.StatusForbidden},
	} {
		rec := guestRequest(t, ok, f.guest, tc.method, tc.path, nil)
		if rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}

	// The shell's background reads come back empty rather than refused, and
	// never reach the handler.
	for path, want := range map[string]string{
		"/api/runtimes":           `[]`,
		"/api/chat/pending-tasks": `{"tasks":[]}`,
		"/api/issue-views":        `[]`,
		"/api/project-folders":    `{"folders":[],"filed":[]}`,
	} {
		rec := guestRequest(t, ok, f.guest, http.MethodGet, path, nil)
		if rec.Code != http.StatusOK || rec.Body.String() != want {
			t.Errorf("GET %s = %d %s, want 200 %s", path, rec.Code, rec.Body.String(), want)
		}
	}
}

func TestGuestCannotRunAgents(t *testing.T) {
	f := newGuestFixture(t)
	ctx := context.Background()
	agentID := dbfx.Agent(t, f.marker+" agent", "", testutil.Cols{"owner_id": f.guest})
	agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	if testHandler.canInvokeAgent(ctx, agent, "member", f.guest, f.guest, testWorkspaceID) {
		t.Error("a guest may invoke the agent it owns, want denied")
	}

	mention := fmt.Sprintf("[@a](mention://agent/%s) look", agentID)
	rec := guestRequest(t, testHandler.CreateComment, f.guest, http.MethodPost,
		"/api/issues/"+f.sharedIssue+"/comments", CreateCommentRequest{Content: mention}, "id", f.sharedIssue)
	if rec.Code != http.StatusForbidden {
		t.Errorf("guest comment mentioning an agent = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	rec = guestRequest(t, testHandler.CreateComment, f.guest, http.MethodPost,
		"/api/issues/"+f.sharedIssue+"/comments", CreateCommentRequest{Content: "plain comment"}, "id", f.sharedIssue)
	if rec.Code != http.StatusCreated {
		t.Errorf("guest plain comment = %d, want 201: %s", rec.Code, rec.Body.String())
	}

	var agents []AgentResponse
	rec = guestRequest(t, testHandler.ListAgents, f.guest, http.MethodGet, "/api/agents", nil)
	rec.Want(http.StatusOK).JSON(&agents)
	for _, a := range agents {
		if a.Instructions != "" || a.RuntimeID != "" || a.Description != "" {
			t.Errorf("guest sees agent %s details: %+v", a.ID, a)
		}
	}
}

func TestGuestInvitationAddsProjects(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	f := newGuestFixture(t)
	email := f.marker + "-invitee@multica.test"
	invitee := dbfx.User(t, "Guest Invitee", email)
	dbfx.Cleanup(t, `DELETE FROM member WHERE user_id = $1`, invitee)

	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members",
		CreateMemberRequest{Email: email, Role: "guest"}), "id", testWorkspaceID)
	testutil.Call(t, testHandler.CreateInvitation, req).Want(http.StatusBadRequest)

	// An admin outside the private project cannot share it.
	admin := createWorkspaceAdmin(t, f.marker+"-admin@multica.test")
	req = withURLParam(asUser(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members",
		CreateMemberRequest{Email: email, Role: "guest", ProjectIDs: []string{f.secret}}), admin), "id", testWorkspaceID)
	testutil.Call(t, testHandler.CreateInvitation, req).Want(http.StatusBadRequest)

	var inv InvitationResponse
	req = withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members",
		CreateMemberRequest{Email: email, Role: "guest", ProjectIDs: []string{f.shared, f.other}}), "id", testWorkspaceID)
	testutil.Call(t, testHandler.CreateInvitation, req).Want(http.StatusCreated).JSON(&inv)
	if inv.Role != "guest" || len(inv.ProjectIDs) != 2 {
		t.Fatalf("invitation = %+v, want a guest invitation for two projects", inv)
	}

	req = withURLParam(asUser(newRequest(http.MethodPost, "/api/invitations/"+inv.ID+"/accept", nil), invitee), "id", inv.ID)
	testutil.Call(t, testHandler.AcceptInvitation, req).Want(http.StatusOK)

	var role string
	dbfx.QueryRow(t, `SELECT role FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, invitee).Scan(&role)
	if role != "guest" {
		t.Fatalf("role = %q, want guest", role)
	}
	if got := guestProjectIDs(t, invitee); fmt.Sprint(got) != fmt.Sprint(sorted(f.shared, f.other)) {
		t.Errorf("guest projects = %v, want shared and other", got)
	}
}

func TestGuestPromotionAndDemotion(t *testing.T) {
	f := newGuestFixture(t)
	path := "/api/workspaces/" + testWorkspaceID + "/members/" + f.guestMember

	var member MemberResponse
	req := withURLParams(newRequest(http.MethodPatch, path, UpdateMemberRequest{Role: "member"}), "id", testWorkspaceID, "memberId", f.guestMember)
	testutil.Call(t, testHandler.UpdateMember, req).Want(http.StatusOK).JSON(&member)
	if member.Role != "member" {
		t.Fatalf("promoted role = %q, want member", member.Role)
	}
	rec := guestRequest(t, testHandler.GetIssue, f.guest, http.MethodGet, "/api/issues/"+f.otherIssue, nil, "id", f.otherIssue)
	if rec.Code != http.StatusOK {
		t.Errorf("promoted member GET other issue = %d, want 200", rec.Code)
	}

	req = withURLParams(newRequest(http.MethodPatch, path, UpdateMemberRequest{Role: "guest"}), "id", testWorkspaceID, "memberId", f.guestMember)
	testutil.Call(t, testHandler.UpdateMember, req).Want(http.StatusBadRequest)

	addProjectMemberRow(t, f.other, f.guest)
	req = withURLParams(newRequest(http.MethodPatch, path, UpdateMemberRequest{Role: "guest", ProjectIDs: []string{f.shared}}),
		"id", testWorkspaceID, "memberId", f.guestMember)
	testutil.Call(t, testHandler.UpdateMember, req).Want(http.StatusOK).JSON(&member)
	if member.Role != "guest" {
		t.Fatalf("demoted role = %q, want guest", member.Role)
	}
	if got := guestProjectIDs(t, f.guest); fmt.Sprint(got) != fmt.Sprint([]string{f.shared}) {
		t.Errorf("demoted guest projects = %v, want only shared", got)
	}
	rec = guestRequest(t, testHandler.GetIssue, f.guest, http.MethodGet, "/api/issues/"+f.otherIssue, nil, "id", f.otherIssue)
	if rec.Code != http.StatusNotFound {
		t.Errorf("demoted guest GET other issue = %d, want 404", rec.Code)
	}

	req = withURLParams(newRequest(http.MethodPut, path+"/projects", SetMemberProjectsRequest{ProjectIDs: []string{f.other}}),
		"id", testWorkspaceID, "memberId", f.guestMember)
	testutil.Call(t, testHandler.SetMemberProjects, req).Want(http.StatusOK)
	if got := guestProjectIDs(t, f.guest); fmt.Sprint(got) != fmt.Sprint([]string{f.other}) {
		t.Errorf("guest projects after set = %v, want only other", got)
	}

	var members []MemberWithUserResponse
	req = withGuestCapability(withURLParam(newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/members", nil), "id", testWorkspaceID))
	testutil.Call(t, testHandler.ListMembersWithUser, req).Want(http.StatusOK).JSON(&members)
	listed := false
	for _, m := range members {
		if m.UserID != f.guest {
			continue
		}
		listed = true
		if fmt.Sprint(m.ProjectIDs) != fmt.Sprint([]string{f.other}) {
			t.Errorf("members list guest project_ids = %v, want only other", m.ProjectIDs)
		}
	}
	if !listed {
		t.Error("members list misses the guest")
	}
}

// Clients released before guests index their role tables by role and crash on
// "guest", so member and invitation lists leave guests out unless the caller
// declares it can render them.
func TestMemberAndInvitationListsHideGuestsFromOlderClients(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	f := newGuestFixture(t)
	req := withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members",
		CreateMemberRequest{Email: f.marker + "-invitee@multica.test", Role: "guest", ProjectIDs: []string{f.shared}}), "id", testWorkspaceID)
	testutil.Call(t, testHandler.CreateInvitation, req).Want(http.StatusCreated)
	req = withURLParam(newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/members",
		CreateMemberRequest{Email: f.marker + "-member@multica.test", Role: "member"}), "id", testWorkspaceID)
	testutil.Call(t, testHandler.CreateInvitation, req).Want(http.StatusCreated)

	for _, tc := range []struct {
		name       string
		capability string
		wantGuests bool
	}{
		{"older client", "", false},
		{"other capabilities only", "stable_attachment_urls", false},
		{"guest-aware client", "stable_attachment_urls, workspace-guests-v1", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withCapability := func(req *http.Request) *http.Request {
				if tc.capability != "" {
					req.Header.Set("X-Client-Capabilities", tc.capability)
				}
				return req
			}

			var members []MemberWithUserResponse
			req := withCapability(withURLParam(newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/members", nil), "id", testWorkspaceID))
			testutil.Call(t, testHandler.ListMembersWithUser, req).Want(http.StatusOK).JSON(&members)
			guests, owner := 0, false
			for _, m := range members {
				if m.Role == projectaccess.RoleGuest {
					guests++
				}
				owner = owner || m.UserID == testUserID
			}
			if !owner {
				t.Error("members list misses the owner")
			}
			if (guests > 0) != tc.wantGuests {
				t.Errorf("members list has %d guests, want guests = %v", guests, tc.wantGuests)
			}

			var invitations []InvitationResponse
			req = withCapability(withURLParam(newRequest(http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/invitations", nil), "id", testWorkspaceID))
			testutil.Call(t, testHandler.ListWorkspaceInvitations, req).Want(http.StatusOK).JSON(&invitations)
			roles := map[string]int{}
			for _, inv := range invitations {
				roles[inv.Role]++
			}
			if roles["member"] != 1 {
				t.Errorf("invitations by role = %v, want the member invitation", roles)
			}
			if (roles[projectaccess.RoleGuest] > 0) != tc.wantGuests {
				t.Errorf("invitations by role = %v, want guests = %v", roles, tc.wantGuests)
			}
		})
	}
}

func withGuestCapability(req *http.Request) *http.Request {
	req.Header.Set("X-Client-Capabilities", protocol.AppCapabilityWorkspaceGuestsV1)
	return req
}

func TestGuestSeesOnlyGuestsItSharesAProjectWith(t *testing.T) {
	f := newGuestFixture(t)
	addGuest := func(email, project string) string {
		userID := createPlainMember(t, f.marker+email)
		dbfx.Exec(t, `UPDATE member SET role = 'guest' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
		addProjectMemberRow(t, project, userID)
		return userID
	}
	colleague := addGuest("-colleague@multica.test", f.shared)
	stranger := addGuest("-stranger@multica.test", f.other)

	var members []MemberWithUserResponse
	guestRequest(t, testHandler.ListMembersWithUser, f.guest, http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/members", nil, "id", testWorkspaceID).
		Want(http.StatusOK).JSON(&members)
	seen := map[string]bool{}
	for _, m := range members {
		seen[m.UserID] = true
		if len(m.ProjectIDs) > 0 {
			t.Errorf("guest sees project_ids of %s", m.Name)
		}
	}
	if !seen[f.guest] || !seen[colleague] {
		t.Errorf("guest misses itself or a guest on its project: %v", seen)
	}
	if seen[stranger] {
		t.Error("guest sees a guest it shares no project with")
	}
}

func guestProjectIDs(t *testing.T, userID string) []string {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT project_id::text FROM project_member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func sorted(ids ...string) []string {
	sort.Strings(ids)
	return ids
}
