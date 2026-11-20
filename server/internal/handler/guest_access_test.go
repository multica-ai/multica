package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A guest reads the workspace it is in through GET /api/workspaces/{id} and
// GET /api/workspaces. Both show it the workspace's name and look, and none of
// the agent instructions, repositories or settings that configure it.
func TestGuestWorkspaceResponseOmitsConfiguration(t *testing.T) {
	suffix := uuid.NewString()[:8]
	workspaceID := dbfx.Workspace(t, "Guest shell "+suffix, "guest-shell-"+suffix, testutil.Cols{
		"context":  "internal agent instructions",
		"settings": `{"github_enabled": true, "github_pr_sidebar_enabled": false, "internal_flag": "x"}`,
		"repos":    `[{"url": "https://github.com/acme/private"}]`,
	})
	dbfx.Cleanup(t, `DELETE FROM member WHERE workspace_id = $1`, workspaceID)
	dbfx.Member(t, workspaceID, testUserID, "owner")
	guest := dbfx.User(t, "Shell guest", "shell-guest-"+suffix+"@multica.test")
	dbfx.Member(t, workspaceID, guest, "guest")
	member := dbfx.User(t, "Shell member", "shell-member-"+suffix+"@multica.test")
	dbfx.Member(t, workspaceID, member, "member")

	get := func(userID string) WorkspaceResponse {
		t.Helper()
		var ws WorkspaceResponse
		req := withURLParam(asUser(newRequest(http.MethodGet, "/api/workspaces/"+workspaceID, nil), userID), "id", workspaceID)
		handler := middleware.RequireWorkspaceMemberFromURL(testHandler.Queries, "id")(http.HandlerFunc(testHandler.GetWorkspace))
		testutil.Call(t, handler.ServeHTTP, req).Want(http.StatusOK).JSON(&ws)
		return ws
	}
	list := func(userID string) WorkspaceResponse {
		t.Helper()
		var all []WorkspaceResponse
		testutil.Call(t, testHandler.ListWorkspaces, asUser(newRequest(http.MethodGet, "/api/workspaces", nil), userID)).
			Want(http.StatusOK).JSON(&all)
		for _, ws := range all {
			if ws.ID == workspaceID {
				return ws
			}
		}
		t.Fatalf("workspace %s missing from the list", workspaceID)
		return WorkspaceResponse{}
	}

	for name, ws := range map[string]WorkspaceResponse{"get": get(guest), "list": list(guest)} {
		if ws.Name != "Guest shell "+suffix || ws.Slug != "guest-shell-"+suffix {
			t.Errorf("guest %s: identity = %q %q", name, ws.Name, ws.Slug)
		}
		if ws.Context != nil {
			t.Errorf("guest %s: context = %q, want null", name, *ws.Context)
		}
		if repos, _ := ws.Repos.([]any); len(repos) != 0 {
			t.Errorf("guest %s: repos = %v, want empty", name, ws.Repos)
		}
		settings, _ := ws.Settings.(map[string]any)
		if fmt.Sprint(settings) != "map[github_enabled:true github_pr_sidebar_enabled:false]" {
			t.Errorf("guest %s: settings = %v, want only the GitHub display flags", name, ws.Settings)
		}
	}

	for _, viewer := range []struct{ name, userID string }{{"owner", testUserID}, {"member", member}} {
		for name, ws := range map[string]WorkspaceResponse{"get": get(viewer.userID), "list": list(viewer.userID)} {
			settings, _ := ws.Settings.(map[string]any)
			if repos, _ := ws.Repos.([]any); ws.Context == nil || len(repos) != 1 || settings["internal_flag"] != "x" {
				t.Errorf("%s %s: want the full workspace, got context=%v repos=%v settings=%v", viewer.name, name, ws.Context, ws.Repos, ws.Settings)
			}
		}
	}
}

// A guest's grants on a private project are listed and changed only by those
// who can see that project, owners and admins included (MUL-9284). Changing a
// guest's projects without seeing one of them leaves the guest in it.
func TestGuestProjectGrantsFollowTheViewersProjects(t *testing.T) {
	clearInvitationsForTestWorkspace(t)
	f := newGuestFixture(t)
	addProjectMemberRow(t, f.secret, f.guest) // the owner (testUserID) is in secret
	admin := createWorkspaceAdmin(t, f.marker+"-admin@multica.test")
	member := createPlainMember(t, f.marker+"-member@multica.test")
	dbfx.Insert(t, "workspace_invitation", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"inviter_id":    testUserID,
		"invitee_email": f.marker + "-invitee@multica.test",
		"role":          "guest",
		"status":        "pending",
		"expires_at":    testutil.Raw("now() + interval '1 day'"),
		"project_ids":   testutil.Raw(fmt.Sprintf("ARRAY['%s', '%s']::uuid[]", f.shared, f.secret)),
	})

	listedGrants := func(viewer string) (member, invitation []string) {
		t.Helper()
		var members []MemberWithUserResponse
		guestRequest(t, testHandler.ListMembersWithUser, viewer, http.MethodGet,
			"/api/workspaces/"+testWorkspaceID+"/members", nil, "id", testWorkspaceID).
			Want(http.StatusOK).JSON(&members)
		for _, m := range members {
			if m.UserID == f.guest {
				member = sorted(m.ProjectIDs...)
			}
		}
		var invitations []InvitationResponse
		guestRequest(t, testHandler.ListWorkspaceInvitations, viewer, http.MethodGet,
			"/api/workspaces/"+testWorkspaceID+"/invitations", nil, "id", testWorkspaceID).
			Want(http.StatusOK).JSON(&invitations)
		for _, inv := range invitations {
			if inv.Role == "guest" {
				invitation = sorted(inv.ProjectIDs...)
			}
		}
		return member, invitation
	}
	both := fmt.Sprint(sorted(f.shared, f.secret))
	for _, tc := range []struct {
		name, viewer, want string
	}{
		{"owner in the private project", testUserID, both},
		{"admin outside it", admin, fmt.Sprint([]string{f.shared})},
		{"member outside it", member, fmt.Sprint([]string{f.shared})},
	} {
		gotMember, gotInvitation := listedGrants(tc.viewer)
		if fmt.Sprint(gotMember) != tc.want {
			t.Errorf("%s: members list guest project_ids = %v, want %s", tc.name, gotMember, tc.want)
		}
		if fmt.Sprint(gotInvitation) != tc.want {
			t.Errorf("%s: invitation project_ids = %v, want %s", tc.name, gotInvitation, tc.want)
		}
	}

	setProjects := func(operator string, ids ...string) *testutil.Response {
		t.Helper()
		path := "/api/workspaces/" + testWorkspaceID + "/members/" + f.guestMember + "/projects"
		req := withURLParams(asUser(newRequest(http.MethodPut, path, SetMemberProjectsRequest{ProjectIDs: ids}), operator),
			"id", testWorkspaceID, "memberId", f.guestMember)
		return testutil.Call(t, testHandler.SetMemberProjects, req)
	}

	// The admin moves the guest to another project; its grant on the private
	// project the admin cannot see stays.
	var resp MemberWithUserResponse
	setProjects(admin, f.other).Want(http.StatusOK).JSON(&resp)
	if got := guestProjectIDs(t, f.guest); fmt.Sprint(got) != fmt.Sprint(sorted(f.other, f.secret)) {
		t.Errorf("after the admin's change guest projects = %v, want other and secret", got)
	}
	if fmt.Sprint(resp.ProjectIDs) != fmt.Sprint([]string{f.other}) {
		t.Errorf("admin's response project_ids = %v, want only other", resp.ProjectIDs)
	}
	setProjects(admin, f.secret).Want(http.StatusBadRequest)

	// The owner, a member of the private project, can take the guest out of it.
	setProjects(testUserID, f.shared).Want(http.StatusOK)
	if got := guestProjectIDs(t, f.guest); fmt.Sprint(got) != fmt.Sprint([]string{f.shared}) {
		t.Errorf("after the owner's change guest projects = %v, want only shared", got)
	}

	// Demoting a member keeps it in the private projects the admin cannot see.
	demoted := createPlainMember(t, f.marker+"-demoted@multica.test")
	addProjectMemberRow(t, f.secret, demoted)
	var demotedMember string
	dbfx.QueryRow(t, `SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, demoted).Scan(&demotedMember)
	req := withURLParams(asUser(newRequest(http.MethodPatch, "/api/workspaces/"+testWorkspaceID+"/members/"+demotedMember,
		UpdateMemberRequest{Role: "guest", ProjectIDs: []string{f.shared}}), admin), "id", testWorkspaceID, "memberId", demotedMember)
	testutil.Call(t, testHandler.UpdateMember, req).Want(http.StatusOK)
	if got := guestProjectIDs(t, demoted); fmt.Sprint(got) != fmt.Sprint(sorted(f.shared, f.secret)) {
		t.Errorf("demoted guest projects = %v, want shared and secret", got)
	}
}

// A member who received doc notifications and is then made a guest cannot
// read them any more: not in the archive, its page and facets, the unread
// counts, or by acting on the item directly. Notifications on issues in its
// projects stay.
func TestDemotedGuestCannotReadProjectlessNotifications(t *testing.T) {
	f := newGuestFixture(t)
	user := createPlainMember(t, f.marker+"-demoted@multica.test")
	notify := func(issueID, title string, archived, read bool) string {
		cols := testutil.Cols{
			"workspace_id": testWorkspaceID, "recipient_type": "member", "recipient_id": user,
			"type": inboxTypeDocComment, "severity": "info", "title": title, "body": "quoted doc text",
			"archived": archived, "read": read,
		}
		if issueID != "" {
			cols["issue_id"] = issueID
			cols["type"] = "mentioned"
		}
		return dbfx.Insert(t, "inbox_item", cols)
	}
	archivedDoc := notify("", f.marker+" doc title", true, false)
	notify("", f.marker+" unread doc", false, false)
	// An archived issue with an active row lists in the inbox, not the archive.
	archivedShared := dbfx.Issue(t, f.marker+" archived shared issue", testutil.Cols{
		"project_id": f.shared, "number": 800000 + int(time.Now().UnixNano()%100000),
	})
	notify(archivedShared, f.marker+" shared", true, false)
	notify(f.sharedIssue, f.marker+" shared unread", false, false)
	notify(f.otherIssue, f.marker+" other unread", false, false)

	archivedTitles := func() map[string]bool {
		t.Helper()
		titles := map[string]bool{}
		var items []InboxItemResponse
		guestRequest(t, testHandler.ListArchivedInbox, user, http.MethodGet, "/api/inbox/archived", nil).Want(http.StatusOK).JSON(&items)
		for _, item := range items {
			titles["list:"+item.Title] = true
		}
		var page archivedInboxPageResponse
		guestRequest(t, testHandler.ListArchivedInboxPage, user, http.MethodGet, "/api/inbox/archived/page", nil).Want(http.StatusOK).JSON(&page)
		for _, item := range page.Items {
			titles["page:"+item.Title] = true
		}
		return titles
	}
	if titles := archivedTitles(); !titles["list:"+f.marker+" doc title"] || !titles["page:"+f.marker+" doc title"] {
		t.Fatalf("as a member the archive misses the doc notification: %v", titles)
	}

	dbfx.Exec(t, `UPDATE member SET role = 'guest' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, user)
	addProjectMemberRow(t, f.shared, user)

	titles := archivedTitles()
	for _, key := range []string{"list:", "page:"} {
		if titles[key+f.marker+" doc title"] {
			t.Errorf("guest archive (%s) still shows the doc notification", key)
		}
		if !titles[key+f.marker+" shared"] {
			t.Errorf("guest archive (%s) lost the notification on its project", key)
		}
	}

	var facets archivedInboxFacetsResponse
	guestRequest(t, testHandler.GetArchivedInboxFacets, user, http.MethodGet, "/api/inbox/archived/facets", nil).Want(http.StatusOK).JSON(&facets)
	if facets.Types["system"] != 0 || facets.UnreadCount != 1 {
		t.Errorf("guest facets count the doc notification: types=%v unread=%d", facets.Types, facets.UnreadCount)
	}

	var count map[string]int64
	guestRequest(t, testHandler.CountUnreadInbox, user, http.MethodGet, "/api/inbox/unread-count", nil).Want(http.StatusOK).JSON(&count)
	if count["count"] != 1 {
		t.Errorf("guest unread count = %d, want 1 (its project's issue only)", count["count"])
	}
	var summary []InboxWorkspaceUnreadResponse
	testutil.Call(t, testHandler.UnreadInboxSummary, asUser(newRequest(http.MethodGet, "/api/inbox/unread-summary", nil), user)).
		Want(http.StatusOK).JSON(&summary)
	if len(summary) != 1 || summary[0].Count != 1 {
		t.Errorf("guest unread summary = %+v, want 1 in the workspace", summary)
	}

	for _, action := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"unarchive", testHandler.UnarchiveInboxItem},
		{"read", testHandler.MarkInboxRead},
	} {
		rec := guestRequest(t, action.handler, user, http.MethodPost, "/api/inbox/"+archivedDoc+"/"+action.name, nil, "id", archivedDoc)
		if rec.Code != http.StatusNotFound {
			t.Errorf("guest %s of the doc notification = %d, want 404: %s", action.name, rec.Code, rec.Body.String())
		}
	}
}

// A member row whose role this build does not know grants nothing: a later
// release may add a role, and rolling back to this build must not read it as
// a member, the way builds before guests read 'guest'.
func TestUnknownMemberRoleGrantsNothing(t *testing.T) {
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `ALTER TABLE member DROP CONSTRAINT IF EXISTS member_role_check,
		ADD CONSTRAINT member_role_check CHECK (role IN ('owner', 'admin', 'member', 'guest', 'future_role')) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM member WHERE role = 'future_role'`)
		testPool.Exec(context.Background(), `ALTER TABLE member DROP CONSTRAINT IF EXISTS member_role_check,
			ADD CONSTRAINT member_role_check CHECK (role IN ('owner', 'admin', 'member', 'guest')) NOT VALID`)
	})
	user := createPlainMember(t, fmt.Sprintf("future-role-%s@multica.test", uuid.NewString()[:8]))
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	guestRequest(t, ok, user, http.MethodGet, "/api/projects", nil).Want(http.StatusNoContent)

	dbfx.Exec(t, `UPDATE member SET role = 'future_role' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, user)

	for _, path := range []string{"/api/projects", "/api/issues", "/api/agents"} {
		if rec := guestRequest(t, ok, user, http.MethodGet, path, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s with an unknown role = %d, want 404", path, rec.Code)
		}
	}
	var all []WorkspaceResponse
	testutil.Call(t, testHandler.ListWorkspaces, asUser(newRequest(http.MethodGet, "/api/workspaces", nil), user)).
		Want(http.StatusOK).JSON(&all)
	for _, ws := range all {
		if ws.ID == testWorkspaceID {
			t.Error("workspace list includes a workspace where the role is unknown")
		}
	}
	rows, err := testHandler.Queries.GetRealtimeMemberships(ctx, db.GetRealtimeMembershipsParams{
		UserIds: []pgtype.UUID{parseUUID(user)}, WorkspaceIds: []pgtype.UUID{parseUUID(testWorkspaceID)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Error("realtime admits a member with an unknown role")
	}

	// Nor does it get workspace data through the account-level reads: the
	// daemon's workspace list, notifications, and the unread summary, which
	// still lists the workspace where the person is a member.
	other := dbfx.Workspace(t, "Future role other "+uuid.NewString()[:8], "future-role-"+uuid.NewString()[:8])
	dbfx.Cleanup(t, `DELETE FROM member WHERE workspace_id = $1`, other)
	dbfx.Member(t, other, user, "member")
	daemonWorkspaces, err := testHandler.Queries.ListDaemonWorkspaces(ctx, parseUUID(user))
	if err != nil {
		t.Fatal(err)
	}
	if len(daemonWorkspaces) != 1 || uuidToString(daemonWorkspaces[0].ID) != other {
		t.Errorf("daemon workspaces = %v, want only the workspace where the role is known", daemonWorkspaces)
	}
	if _, err := testHandler.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
		ID: parseUUID(uuid.NewString()), WorkspaceID: parseUUID(testWorkspaceID), RecipientType: "member",
		RecipientID: parseUUID(user), Type: "mentioned", Severity: "info", Title: "future role",
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("deliver to a member with an unknown role: err = %v, want no row written", err)
	}
	for _, ws := range []string{testWorkspaceID, other} {
		dbfx.Insert(t, "inbox_item", testutil.Cols{
			"workspace_id": ws, "recipient_type": "member", "recipient_id": user,
			"type": "mentioned", "severity": "info", "title": "unread",
		})
	}
	var summary []InboxWorkspaceUnreadResponse
	testutil.Call(t, testHandler.UnreadInboxSummary, asUser(newRequest(http.MethodGet, "/api/inbox/unread-summary", nil), user)).
		Want(http.StatusOK).JSON(&summary)
	if len(summary) != 1 || summary[0].WorkspaceID != other {
		t.Errorf("unread summary = %+v, want only the workspace where the role is known", summary)
	}
}

// A pending guest invitation reserves no member slot, so a joiner holding one
// who comes in through a member share link takes a new slot, and cannot take
// one another invitation reserved.
func TestJoinShareLinkWithGuestInvitationTakesANewSlot(t *testing.T) {
	workspaceID := memberLimitWorkspace(t, 3) // owner + 3 members
	link := createTestShareLink(t, workspaceID, "member", 5, 0)
	email := "guest-invited-joiner-" + uuid.NewString() + "@multica.ai"
	userID := dbfx.User(t, "Guest-invited joiner", email)
	guestInvitation := pendingInvitation(t, workspaceID, email, userID)
	dbfx.Exec(t, `UPDATE workspace_invitation SET role = 'guest' WHERE id = $1`, guestInvitation)
	pendingInvitation(t, workspaceID, "other-"+uuid.NewString()+"@multica.ai", "") // the fifth slot
	useMemberLimit(t, workspaceID, entitlement.ActionEnforce, 5)

	wantMemberLimitReached(t, joinWorkspaceByLink(t, link.Code, userID))
	if n := dbfx.Count(t, `SELECT count(*) FROM member WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID); n != 0 {
		t.Fatalf("refused join created %d members", n)
	}
}

// The daemon routes hand out the same repositories and settings. A guest's
// PAT/JWT is refused by the guest allowlist, and the membership cache, which
// holds no role, never lists a guest; the reads that return the
// configuration also check the current role of the person behind the
// credential, which covers a daemon token whose owner has become a guest.
func TestGuestCannotReadWorkspaceConfigThroughDaemonRoutes(t *testing.T) {
	setHandlerTestWorkspaceRepos(t, []map[string]string{{"url": "git@example.com:acme/private.git"}})
	marker := fmt.Sprintf("daemon-guest-%d", time.Now().UnixNano())
	guest := createPlainMember(t, marker+"@multica.test")
	dbfx.Exec(t, `UPDATE member SET role = 'guest' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, guest)
	member := createPlainMember(t, marker+"-member@multica.test")
	path := "/api/daemon/workspaces/" + testWorkspaceID + "/repos"

	readRepos := func(req *http.Request) *testutil.Response {
		t.Helper()
		return testutil.Call(t, testHandler.GetDaemonWorkspaceRepos, withURLParam(req, "workspaceId", testWorkspaceID))
	}
	guestToken := func(method, path string, body any) *http.Request {
		req := newDaemonTokenRequest(method, path, body, testWorkspaceID, marker)
		return req.WithContext(middleware.WithDaemonTokenOwnerID(req.Context(), guest))
	}
	guestPAT := func() *http.Request { return asUser(newRequest(http.MethodGet, path, nil), guest) }

	readRepos(guestPAT()).Want(http.StatusForbidden)
	readRepos(guestToken(http.MethodGet, path, nil)).Want(http.StatusNotFound)
	readRepos(asUser(newRequest(http.MethodGet, path, nil), member)).Want(http.StatusOK)
	readRepos(newDaemonTokenRequest(http.MethodGet, path, nil, testWorkspaceID, "owner-daemon")).Want(http.StatusOK)
	if testHandler.verifyDaemonWorkspaceAccess(guestPAT(), testWorkspaceID) {
		t.Error("the batch daemon check admits a guest")
	}

	// A daemon token whose owner is now a guest still registers its runtimes
	// but gets none of the workspace's repositories or settings.
	dbfx.Cleanup(t, `DELETE FROM agent_runtime WHERE daemon_id = $1`, marker)
	var registered map[string]any
	rec := testutil.Call(t, testHandler.DaemonRegister, guestToken(http.MethodPost, "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    marker,
		"device_name":  "guest-device",
		"runtimes":     []map[string]any{{"name": "guest-runtime", "type": "claude", "version": "1.0.0", "status": "online"}},
	})).Want(http.StatusOK)
	if err := json.NewDecoder(rec.Body).Decode(&registered); err != nil {
		t.Fatal(err)
	}
	if repos, _ := registered["repos"].([]any); len(repos) != 0 || registered["settings"] != nil {
		t.Errorf("guest-owned registration returned repos=%v settings=%v, want none", registered["repos"], registered["settings"])
	}

	t.Run("membership cache", func(t *testing.T) {
		installFreshMembershipCache(t)
		ctx := context.Background()
		testHandler.verifyDaemonWorkspaceAccess(guestPAT(), testWorkspaceID)
		if testHandler.MembershipCache.Get(ctx, guest, testWorkspaceID) {
			t.Error("a guest was cached as a member")
		}
		// An entry written before this rule, or by a missed path, still
		// does not open the configuration.
		testHandler.MembershipCache.Set(ctx, guest, testWorkspaceID)
		readRepos(guestPAT()).Want(http.StatusNotFound)
	})
}
