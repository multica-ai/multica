package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// MUL-9284: only the member list opens a private project. Its lead is as free
// as any project's (a person, an agent or nobody), so who manages the member
// list is settled apart from it: see canManageProjectAccess.

type projectLead struct{ leadType, leadID string }

func loadProjectLead(t *testing.T, projectID string) projectLead {
	t.Helper()
	var leadType, leadID *string
	if err := testPool.QueryRow(context.Background(),
		`SELECT lead_type, lead_id::text FROM project WHERE id = $1`, projectID).Scan(&leadType, &leadID); err != nil {
		t.Fatalf("load project lead: %v", err)
	}
	var out projectLead
	if leadType != nil {
		out.leadType = *leadType
	}
	if leadID != nil {
		out.leadID = *leadID
	}
	return out
}

func projectMemberIDs(t *testing.T, projectID string) map[string]bool {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `SELECT user_id::text FROM project_member WHERE project_id = $1`, projectID)
	if err != nil {
		t.Fatalf("list project members: %v", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out[id] = true
	}
	return out
}

func updateProjectAs(t *testing.T, userID, projectID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := withURLParam(asUser(newRequest(http.MethodPut, "/api/projects/"+projectID, body), userID), "id", projectID)
	return testutil.Call(t, testHandler.UpdateProject, req)
}

// The unread badge is computed in SQL, apart from the request-level hidden
// set, so it needs its own proof that a role no longer counts.
func TestPrivateProjectUnreadCountIgnoresWorkspaceRole(t *testing.T) {
	f := newPrivateProjectFixture(t)
	count := func(userID string) int64 {
		t.Helper()
		var resp struct {
			Count int64 `json:"count"`
		}
		req := asUser(newRequest(http.MethodGet, "/api/inbox/unread-count", nil), userID)
		testutil.Call(t, inboxWorkspaceHandler(testHandler.CountUnreadInbox), req).Want(http.StatusOK).JSON(&resp)
		return resp.Count
	}
	before := count(f.outsideAdmin)
	// Delivered while the admin's role still let them see the project.
	insertSecretInboxItem(t, f, f.outsideAdmin)
	if got := count(f.outsideAdmin); got != before {
		t.Fatalf("admin outside the project: unread count = %d, want %d (private notification not counted)", got, before)
	}
	addProjectMemberRow(t, f.projectID, f.outsideAdmin)
	if got := count(f.outsideAdmin); got != before+1 {
		t.Fatalf("admin in the member list: unread count = %d, want %d", got, before+1)
	}
}

// projectManagerID asks the members endpoint, as a project member, who
// manages the project's access.
func projectManagerID(t *testing.T, userID, projectID string) string {
	t.Helper()
	var resp struct {
		ManagerID *string `json:"manager_id"`
	}
	req := withURLParam(asUser(newRequest(http.MethodGet, "/api/projects/"+projectID+"/members", nil), userID), "id", projectID)
	testutil.Call(t, testHandler.ListProjectMembers, req).Want(http.StatusOK).JSON(&resp)
	if resp.ManagerID == nil {
		return ""
	}
	return *resp.ManagerID
}

func addProjectMembersAs(t *testing.T, userID, projectID string, ids ...string) *testutil.Response {
	t.Helper()
	req := withURLParam(asUser(newRequest(http.MethodPost, "/api/projects/"+projectID+"/members", map[string]any{"user_ids": ids}), userID), "id", projectID)
	return testutil.Call(t, testHandler.AddProjectMembers, req)
}

func TestPrivateProjectLeadIsOptionalAndMayBeAnAgent(t *testing.T) {
	f := newPrivateProjectFixture(t)
	agentID := dbfx.Agent(t, f.marker+" lead agent", testRuntimeID)
	agentLead := projectLead{"agent", agentID}

	create := func(t *testing.T, body map[string]any) string {
		t.Helper()
		body["title"] = f.marker + " created"
		body["visibility"] = "private"
		var project ProjectResponse
		testutil.Call(t, testHandler.CreateProject, asUser(newRequest(http.MethodPost, "/api/projects", body), f.insider)).
			Want(http.StatusCreated).JSON(&project)
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = $1`, project.ID)
			testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, project.ID)
		})
		return project.ID
	}
	t.Run("created with no lead", func(t *testing.T) {
		projectID := create(t, map[string]any{})
		if got := loadProjectLead(t, projectID); got != (projectLead{}) {
			t.Fatalf("lead = %+v, want none", got)
		}
		if members := projectMemberIDs(t, projectID); len(members) != 1 || !members[f.insider] {
			t.Fatalf("members = %v, want only the creator", members)
		}
	})
	t.Run("created with an agent lead", func(t *testing.T) {
		projectID := create(t, map[string]any{"lead_type": "agent", "lead_id": agentID})
		if got := loadProjectLead(t, projectID); got != agentLead {
			t.Fatalf("lead = %+v, want the agent", got)
		}
		if members := projectMemberIDs(t, projectID); len(members) != 1 || !members[f.insider] {
			t.Fatalf("members = %v, want only the creator", members)
		}
	})
	t.Run("created with another member as lead", func(t *testing.T) {
		projectID := create(t, map[string]any{"lead_type": "member", "lead_id": f.outsider})
		if got := loadProjectLead(t, projectID); got != (projectLead{"member", f.outsider}) {
			t.Fatalf("lead = %+v, want the named member", got)
		}
		if members := projectMemberIDs(t, projectID); len(members) != 2 || !members[f.outsider] || !members[f.insider] {
			t.Fatalf("members = %v, want the creator and the lead", members)
		}
	})

	t.Run("handed to an agent, then cleared", func(t *testing.T) {
		dbfx.Exec(t, `UPDATE project SET lead_type = 'member', lead_id = $1 WHERE id = $2`, f.insider, f.projectID)
		updateProjectAs(t, f.insider, f.projectID, map[string]any{"lead_type": "agent", "lead_id": agentID}).Want(http.StatusOK)
		if got := loadProjectLead(t, f.projectID); got != agentLead {
			t.Fatalf("lead = %+v, want the agent", got)
		}
		updateProjectAs(t, testUserID, f.projectID, map[string]any{"lead_type": nil, "lead_id": nil}).Want(http.StatusOK)
		if got := loadProjectLead(t, f.projectID); got != (projectLead{}) {
			t.Fatalf("lead = %+v, want none", got)
		}
		if members := projectMemberIDs(t, f.projectID); len(members) != 2 || !members[testUserID] || !members[f.insider] {
			t.Fatalf("members = %v, want them unchanged", members)
		}
	})
}

// Making a project private leaves its lead alone: it adds the caller, and the
// lead when that is a person who can still be added.
func TestProjectTurningPrivateKeepsItsLead(t *testing.T) {
	marker := newPrivateProjectFixture(t).marker
	agentID := dbfx.Agent(t, marker+" lead agent", testRuntimeID)
	human := createPlainMember(t, marker+"-human-lead@multica.test")
	// A lead who has since left the workspace: the id points at nobody.
	departed := "00000000-0000-0000-0000-0000000d0001"

	for _, tc := range []struct {
		name        string
		lead        projectLead
		body        map[string]any
		wantLead    projectLead
		wantMembers []string
	}{
		{"no lead", projectLead{}, nil, projectLead{}, []string{testUserID}},
		{"agent lead", projectLead{"agent", agentID}, nil, projectLead{"agent", agentID}, []string{testUserID}},
		{"human lead", projectLead{"member", human}, nil, projectLead{"member", human}, []string{testUserID, human}},
		{"lead who left the workspace", projectLead{"member", departed}, nil, projectLead{"member", departed}, []string{testUserID}},
		{"person named in the same request", projectLead{}, map[string]any{"lead_type": "member", "lead_id": human}, projectLead{"member", human}, []string{testUserID, human}},
		{"agent named in the same request", projectLead{"member", human}, map[string]any{"lead_type": "agent", "lead_id": agentID}, projectLead{"agent", agentID}, []string{testUserID}},
		{"lead cleared in the same request", projectLead{"member", human}, map[string]any{"lead_type": nil, "lead_id": nil}, projectLead{}, []string{testUserID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cols := testutil.Cols{}
			if tc.lead.leadType != "" {
				cols["lead_type"], cols["lead_id"] = tc.lead.leadType, tc.lead.leadID
			}
			projectID := dbfx.Project(t, marker+" turning private", cols)
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = $1`, projectID)
			})
			body := map[string]any{"visibility": "private"}
			for k, v := range tc.body {
				body[k] = v
			}
			updateProjectAs(t, testUserID, projectID, body).Want(http.StatusOK)
			if n := dbfx.Count(t, `SELECT count(*) FROM project WHERE id = $1 AND visibility = 'private'`, projectID); n != 1 {
				t.Fatal("project did not turn private")
			}
			if got := loadProjectLead(t, projectID); got != tc.wantLead {
				t.Fatalf("lead = %+v, want %+v", got, tc.wantLead)
			}
			members := projectMemberIDs(t, projectID)
			if len(members) != len(tc.wantMembers) {
				t.Fatalf("members = %v, want %v", members, tc.wantMembers)
			}
			for _, id := range tc.wantMembers {
				if !members[id] {
					t.Fatalf("members = %v, want %v", members, tc.wantMembers)
				}
			}
		})
	}

	// A departed user named as the new lead is still refused, and nothing
	// about the project changes.
	projectID := dbfx.Project(t, marker+" refused")
	updateProjectAs(t, testUserID, projectID, map[string]any{"visibility": "private", "lead_type": "member", "lead_id": departed}).Want(http.StatusBadRequest)
	if n := dbfx.Count(t, `SELECT count(*) FROM project WHERE id = $1 AND visibility = 'private'`, projectID); n != 0 {
		t.Fatal("a refused update still made the project private")
	}
}

// Who manages a private project's member list when no workspace owner or
// admin is on it: the lead if a person on the list, else the creator if on
// the list, else whoever has been on it longest.
func TestPrivateProjectAccessManager(t *testing.T) {
	marker := newPrivateProjectFixture(t).marker
	agentID := dbfx.Agent(t, marker+" lead agent", testRuntimeID)
	lead := createPlainMember(t, marker+"-mgr-lead@multica.test")
	creator := createPlainMember(t, marker+"-mgr-creator@multica.test")
	early := createPlainMember(t, marker+"-mgr-early@multica.test")
	late := createPlainMember(t, marker+"-mgr-late@multica.test")
	admin := createWorkspaceAdmin(t, marker+"-mgr-admin@multica.test")
	// Not on any list: the one each manager is asked to add.
	newcomer := createPlainMember(t, marker+"-mgr-newcomer@multica.test")

	// Members are listed oldest first; ages are a day apart.
	for _, tc := range []struct {
		name    string
		cols    testutil.Cols
		members []string
		want    string
	}{
		{
			"a person who leads it",
			testutil.Cols{"lead_type": "member", "lead_id": lead, "creator_type": "member", "creator_id": creator},
			[]string{early, creator, lead}, lead,
		},
		{
			"agent lead: the creator",
			testutil.Cols{"lead_type": "agent", "lead_id": agentID, "creator_type": "member", "creator_id": creator},
			[]string{early, creator, late}, creator,
		},
		{
			"no lead: the creator",
			testutil.Cols{"creator_type": "member", "creator_id": creator},
			[]string{early, creator, late}, creator,
		},
		{
			"lead not on the list: the creator",
			testutil.Cols{"lead_type": "member", "lead_id": lead, "creator_type": "member", "creator_id": creator},
			[]string{early, creator}, creator,
		},
		{
			"creator not on the list: the longest-standing member",
			testutil.Cols{"lead_type": "agent", "lead_id": agentID, "creator_type": "member", "creator_id": creator},
			[]string{early, late}, early,
		},
		{
			"created by an agent: the longest-standing member",
			// An agent's id never matches a user, but the type is what rules it out.
			testutil.Cols{"creator_type": "agent", "creator_id": late},
			[]string{early, late}, early,
		},
		{
			"no creator recorded: the longest-standing member",
			testutil.Cols{},
			[]string{early, late}, early,
		},
		{
			"an admin on the list does not displace the manager",
			testutil.Cols{"creator_type": "member", "creator_id": creator},
			[]string{admin, creator, late}, creator,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.cols["visibility"] = projectaccess.VisibilityPrivate
			projectID := dbfx.Project(t, marker+" managed", tc.cols)
			t.Cleanup(func() {
				testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = $1`, projectID)
			})
			for i, userID := range tc.members {
				addProjectMemberRow(t, projectID, userID)
				dbfx.Exec(t, `UPDATE project_member SET created_at = now() - make_interval(days => $3) WHERE project_id = $1 AND user_id = $2`,
					projectID, userID, len(tc.members)-i)
			}
			for _, userID := range tc.members {
				if got := projectManagerID(t, userID, projectID); got != tc.want {
					t.Fatalf("manager_id as seen by %s = %q, want %q", userID, got, tc.want)
				}
			}
			// The rule is enforced, not just reported: every write that
			// changes who sees the project takes the manager, or an owner or
			// admin who is on the list.
			for _, userID := range tc.members {
				if userID == tc.want || userID == admin {
					continue
				}
				addProjectMembersAs(t, userID, projectID, newcomer).Want(http.StatusForbidden)
				req := testutil.WithURLParams(asUser(newRequest(http.MethodDelete, "/api/projects/"+projectID+"/members/"+userID, nil), userID), "id", projectID, "userId", userID)
				testutil.Call(t, testHandler.RemoveProjectMember, req).Want(http.StatusForbidden)
				updateProjectAs(t, userID, projectID, map[string]any{"visibility": "workspace"}).Want(http.StatusForbidden)
				updateProjectAs(t, userID, projectID, map[string]any{"lead_type": "member", "lead_id": userID}).Want(http.StatusForbidden)
			}
			if members := projectMemberIDs(t, projectID); len(members) != len(tc.members) || members[newcomer] {
				t.Fatalf("refused requests changed the member list: %v", members)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM project WHERE id = $1 AND visibility = 'private'`, projectID); n != 1 {
				t.Fatal("a refused request made the project workspace-visible")
			}
			for _, userID := range tc.members {
				if userID != admin {
					continue
				}
				addProjectMembersAs(t, admin, projectID, newcomer).Want(http.StatusOK)
				removeProjectMemberRow(t, projectID, newcomer)
			}
			addProjectMembersAs(t, tc.want, projectID, newcomer).Want(http.StatusOK)
			if !projectMemberIDs(t, projectID)[newcomer] {
				t.Fatal("the manager's add did not add the member")
			}
		})
	}

	t.Run("people added together: the one who added the rest", func(t *testing.T) {
		projectID := dbfx.Project(t, marker+" tied", testutil.Cols{"visibility": projectaccess.VisibilityPrivate})
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = $1`, projectID)
		})
		// One statement, as when a project is made private with a member
		// list: every row gets the same join time.
		dbfx.Exec(t, `INSERT INTO project_member (workspace_id, project_id, user_id, added_by)
			SELECT $1, $2, u, $3 FROM unnest($4::uuid[]) AS u`, testWorkspaceID, projectID, late, []string{early, late, creator})
		if got := projectManagerID(t, early, projectID); got != late {
			t.Fatalf("manager_id = %q, want the member who added the others (%q)", got, late)
		}
	})

	t.Run("nobody for an empty list or a workspace-visible project", func(t *testing.T) {
		ctx := context.Background()
		empty := dbfx.Project(t, marker+" empty", testutil.Cols{
			"visibility": projectaccess.VisibilityPrivate, "lead_type": "member", "lead_id": lead, "creator_type": "member", "creator_id": creator,
		})
		open := dbfx.Project(t, marker+" open", testutil.Cols{"lead_type": "member", "lead_id": lead})
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = $1`, open)
		})
		// Rows of a project that is no longer private are kept, and count
		// for nothing while it stays workspace-visible.
		addProjectMemberRow(t, open, lead)
		for name, projectID := range map[string]string{"empty": empty, "open": open} {
			project, err := testHandler.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
				ID: parseUUID(projectID), WorkspaceID: parseUUID(testWorkspaceID),
			})
			if err != nil {
				t.Fatalf("%s: load project: %v", name, err)
			}
			manager, err := testHandler.projectAccessManager(ctx, project)
			if err != nil || manager.Valid {
				t.Fatalf("%s: manager = %s, err = %v; want none", name, uuidToString(manager), err)
			}
		}
		if got := projectManagerID(t, lead, open); got != "" {
			t.Fatalf("workspace-visible project: manager_id = %q, want none", got)
		}
	})
}

// A member who leaves the workspace leaves every member list. Nothing else
// about their projects is rewritten: whoever is next in line manages access.
func TestPrivateProjectAccessManagerAfterLeadLeavesWorkspace(t *testing.T) {
	ctx := context.Background()
	marker := newPrivateProjectFixture(t).marker
	leaver := createPlainMember(t, marker+"-leaver@multica.test")
	early := createPlainMember(t, marker+"-early@multica.test")
	late := createPlainMember(t, marker+"-late@multica.test")
	newcomer := createPlainMember(t, marker+"-newcomer@multica.test")
	var leaverMemberID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, leaver).Scan(&leaverMemberID); err != nil {
		t.Fatalf("load member row: %v", err)
	}

	led := testutil.Cols{"visibility": projectaccess.VisibilityPrivate, "lead_type": "member", "lead_id": leaver}
	shared := dbfx.Project(t, marker+" shared", led)
	solo := dbfx.Project(t, marker+" solo", led)
	// Created by someone who stays, alongside a member who joined earlier.
	founded := dbfx.Project(t, marker+" founded", testutil.Cols{
		"visibility": projectaccess.VisibilityPrivate, "lead_type": "member", "lead_id": leaver,
		"creator_type": "member", "creator_id": late,
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = ANY($1::uuid[])`, []string{shared, solo, founded})
	})
	addProjectMemberRow(t, solo, leaver)
	for _, projectID := range []string{shared, founded} {
		for i, userID := range []string{leaver, late, early} {
			addProjectMemberRow(t, projectID, userID)
			dbfx.Exec(t, `UPDATE project_member SET created_at = now() - make_interval(days => $3) WHERE project_id = $1 AND user_id = $2`, projectID, userID, i*10)
		}
	}
	if got := projectManagerID(t, early, shared); got != leaver {
		t.Fatalf("manager before the lead left = %q, want the lead", got)
	}

	if _, err := testHandler.revokeAndRemoveMember(ctx, parseUUID(testWorkspaceID), parseUUID(leaver), parseUUID(leaverMemberID), parseUUID(testUserID)); err != nil {
		t.Fatalf("revokeAndRemoveMember: %v", err)
	}

	for _, projectID := range []string{shared, solo, founded} {
		if got := loadProjectLead(t, projectID); got != (projectLead{"member", leaver}) {
			t.Errorf("lead = %+v, want it left as it was", got)
		}
		if projectMemberIDs(t, projectID)[leaver] {
			t.Error("the leaver is still on a member list")
		}
	}
	if got := projectManagerID(t, late, shared); got != early {
		t.Errorf("shared project manager = %q, want its longest-standing remaining member", got)
	}
	if got := projectManagerID(t, early, founded); got != late {
		t.Errorf("founded project manager = %q, want its creator", got)
	}
	addProjectMembersAs(t, late, shared, newcomer).Want(http.StatusForbidden)
	addProjectMembersAs(t, early, shared, newcomer).Want(http.StatusOK)
	// The new manager can also name a lead who is still here.
	updateProjectAs(t, early, shared, map[string]any{"lead_type": "member", "lead_id": late}).Want(http.StatusOK)
	if got := projectManagerID(t, early, shared); got != late {
		t.Errorf("manager after naming a new lead = %q, want that lead", got)
	}
	// Nobody is left on this one. It stays private; the workspace owner is
	// the way back in (TestWorkspaceOwnerJoinsPrivateProject).
	if members := projectMemberIDs(t, solo); len(members) != 0 {
		t.Errorf("solo project members = %v, want none", members)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM project WHERE id = $1 AND visibility = 'private'`, solo); n != 1 {
		t.Error("solo project is no longer private")
	}
}

// A local search index built while owners and admins still saw every private
// project holds those projects, and the rule change wrote no change row that
// would remove them. Its cursor carries no access rules number, which is how
// the server tells it to rebuild.
func TestSearchIndexCopyFromRoleBasedAccessIsRebuilt(t *testing.T) {
	f := newPrivateProjectFixture(t)
	current := searchIndexManifest(t, testWorkspaceID).Cursor
	issued, err := decodeSearchIndexCursor(current)
	if err != nil {
		t.Fatalf("decode manifest cursor: %v", err)
	}
	if issued.AccessRules != searchIndexAccessRules {
		t.Fatalf("manifest cursor access rules = %d, want %d", issued.AccessRules, searchIndexAccessRules)
	}
	raw, _ := json.Marshal(map[string]any{"v": searchIndexCursorVersion, "s": issued.Since})
	legacy := base64.RawURLEncoding.EncodeToString(raw)

	changes := func(userID, cursor string) *testutil.Response {
		req := asUser(searchIndexRequest(http.MethodPost, "/api/search-index/changes", testWorkspaceID, map[string]any{"cursor": cursor}), userID)
		return testutil.Call(t, testHandler.ListSearchIndexChanges, req)
	}

	// The admin is not on the member list: whatever the old copy holds of the
	// project has to go, and a copy built under the current rules is fine.
	changes(f.outsideAdmin, legacy).Want(http.StatusGone)
	changes(f.outsideAdmin, current).Want(http.StatusOK)

	// Someone who sees every project has nothing to drop: the old cursor
	// keeps working and comes back as a current one.
	var resp SearchIndexChangesResponse
	changes(f.insider, legacy).Want(http.StatusOK).JSON(&resp)
	next, err := decodeSearchIndexCursor(resp.Cursor)
	if err != nil {
		t.Fatalf("decode returned cursor: %v", err)
	}
	if next.AccessRules != searchIndexAccessRules {
		t.Errorf("returned cursor access rules = %d, want %d", next.AccessRules, searchIndexAccessRules)
	}
}

// MUL-9325: PUT /api/projects/{id} used to read member_ids only when the
// visibility changed, so naming members for a project that was already
// private succeeded and added nobody. Older CLIs still send exactly that.
func TestProjectUpdateMemberIDsAreNeverDropped(t *testing.T) {
	f := newPrivateProjectFixture(t)
	dbfx.Exec(t, `UPDATE project SET lead_type = 'member', lead_id = $1 WHERE id = $2`, f.insider, f.projectID)
	plain := createPlainMember(t, f.marker+"-plain@multica.test")
	addProjectMemberRow(t, f.projectID, plain)
	newcomer := createPlainMember(t, f.marker+"-newcomer@multica.test")
	title := func() string {
		var s string
		if err := testPool.QueryRow(context.Background(), `SELECT title FROM project WHERE id = $1`, f.projectID).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	visibility := func() string {
		var s string
		if err := testPool.QueryRow(context.Background(), `SELECT visibility FROM project WHERE id = $1`, f.projectID).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	refused := map[string]any{"visibility": "private", "member_ids": []string{newcomer}, "title": "must not change"}
	assertUnchanged := func(t *testing.T) {
		t.Helper()
		if projectMemberIDs(t, f.projectID)[newcomer] || title() == "must not change" {
			t.Fatal("a refused update still changed the project")
		}
	}

	t.Run("callers who could not add members directly are refused alike", func(t *testing.T) {
		const denied = "only workspace admins and the member who manages this project's access can add members"
		addProjectMembersAs(t, plain, f.projectID, newcomer).Want(http.StatusForbidden)
		if body := updateProjectAs(t, plain, f.projectID, refused).Want(http.StatusForbidden).Text(); !strings.Contains(body, denied) {
			t.Fatalf("non-manager: body = %s, want the member endpoint's message", body)
		}
		assertUnchanged(t)

		agent := dbfx.Agent(t, f.marker+" insider agent", testRuntimeID, testutil.Cols{"owner_id": f.insider})
		task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": testRuntimeID, "issue_id": f.secretIssue, "status": "running",
			"originator_user_id": f.insider, "accountable_user_id": f.insider})
		req := asUser(newRequest(http.MethodPut, "/api/projects/"+f.projectID, refused), f.insider)
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Agent-ID", agent)
		req.Header.Set("X-Task-ID", task)
		testutil.Call(t, testHandler.UpdateProject, withURLParam(req, "id", f.projectID)).Want(http.StatusForbidden)
		assertUnchanged(t)

		updateProjectAs(t, f.insider, f.projectID, map[string]any{"member_ids": []string{"00000000-0000-0000-0000-00000000beef"}, "title": "must not change"}).
			Want(http.StatusBadRequest)
		assertUnchanged(t)
	})

	t.Run("the access manager adds them, with or without restating the visibility", func(t *testing.T) {
		t.Cleanup(func() { removeProjectMemberRow(t, f.projectID, newcomer) })
		updateProjectAs(t, f.insider, f.projectID, map[string]any{"visibility": "private", "member_ids": []string{newcomer}}).Want(http.StatusOK)
		if !projectMemberIDs(t, f.projectID)[newcomer] {
			t.Fatal("member_ids with an unchanged visibility added nobody")
		}
		removeProjectMemberRow(t, f.projectID, newcomer)
		updateProjectAs(t, testUserID, f.projectID, map[string]any{"member_ids": []string{newcomer}}).Want(http.StatusOK)
		if !projectMemberIDs(t, f.projectID)[newcomer] {
			t.Fatal("member_ids without visibility added nobody")
		}
	})

	t.Run("together with a new lead who is not yet a member", func(t *testing.T) {
		t.Cleanup(func() {
			dbfx.Exec(t, `UPDATE project SET lead_type = 'member', lead_id = $1 WHERE id = $2`, f.insider, f.projectID)
			removeProjectMemberRow(t, f.projectID, newcomer)
			removeProjectMemberRow(t, f.projectID, f.outsider)
		})
		updateProjectAs(t, f.insider, f.projectID, map[string]any{"lead_type": "member", "lead_id": f.outsider, "member_ids": []string{newcomer}}).
			Want(http.StatusOK)
		if members := projectMemberIDs(t, f.projectID); !members[newcomer] || !members[f.outsider] {
			t.Fatalf("members = %v, want the named member and the new lead", members)
		}
	})

	t.Run("a project made workspace-visible takes no new members", func(t *testing.T) {
		updateProjectAs(t, testUserID, f.projectID, map[string]any{"visibility": "workspace", "member_ids": []string{newcomer}}).
			Want(http.StatusBadRequest)
		if visibility() != projectaccess.VisibilityPrivate || projectMemberIDs(t, f.projectID)[newcomer] {
			t.Fatal("a refused switch still changed the project")
		}
		// The web app's access dialog restates the current list.
		updateProjectAs(t, testUserID, f.projectID, map[string]any{"visibility": "workspace", "member_ids": []string{testUserID, f.insider, plain}}).
			Want(http.StatusOK)
		if visibility() != projectaccess.VisibilityWorkspace {
			t.Fatal("project did not turn workspace-visible")
		}
		updateProjectAs(t, testUserID, f.projectID, map[string]any{"member_ids": []string{newcomer}}).Want(http.StatusBadRequest)
		if projectMemberIDs(t, f.projectID)[newcomer] {
			t.Fatal("a workspace-visible project gained a member")
		}
	})
}
