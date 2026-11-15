package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// MUL-9284: only the member list opens a private project, and a private
// project always has a human lead.

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

func TestPrivateProjectCreateAlwaysHasHumanLead(t *testing.T) {
	f := newPrivateProjectFixture(t)
	agentID := dbfx.Agent(t, f.marker+" lead agent", testRuntimeID)
	create := func(userID string, body map[string]any) *testutil.Response {
		body["title"] = f.marker + " created"
		body["visibility"] = "private"
		return testutil.Call(t, testHandler.CreateProject, asUser(newRequest(http.MethodPost, "/api/projects", body), userID))
	}
	created := func(resp *testutil.Response) ProjectResponse {
		t.Helper()
		var project ProjectResponse
		resp.Want(http.StatusCreated).JSON(&project)
		t.Cleanup(func() {
			testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = $1`, project.ID)
			testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, project.ID)
		})
		return project
	}

	t.Run("no lead named: the creator leads", func(t *testing.T) {
		project := created(create(f.insider, map[string]any{}))
		if got := loadProjectLead(t, project.ID); got != (projectLead{"member", f.insider}) {
			t.Fatalf("lead = %+v, want the creator", got)
		}
	})
	t.Run("another member named: they lead and are a member", func(t *testing.T) {
		project := created(create(f.insider, map[string]any{"lead_type": "member", "lead_id": f.outsider}))
		if got := loadProjectLead(t, project.ID); got != (projectLead{"member", f.outsider}) {
			t.Fatalf("lead = %+v, want the named member", got)
		}
		if members := projectMemberIDs(t, project.ID); !members[f.outsider] || !members[f.insider] {
			t.Fatalf("members = %v, want the creator and the lead", members)
		}
	})
	t.Run("agent lead refused", func(t *testing.T) {
		before := dbfx.Count(t, `SELECT count(*) FROM project WHERE workspace_id = $1`, testWorkspaceID)
		create(f.insider, map[string]any{"lead_type": "agent", "lead_id": agentID}).Want(http.StatusBadRequest)
		if after := dbfx.Count(t, `SELECT count(*) FROM project WHERE workspace_id = $1`, testWorkspaceID); after != before {
			t.Fatal("a refused create still stored a project")
		}
	})
}

func TestPrivateProjectLeadCannotBeClearedOrGivenToAnAgent(t *testing.T) {
	f := newPrivateProjectFixture(t)
	agentID := dbfx.Agent(t, f.marker+" lead agent", testRuntimeID)
	dbfx.Exec(t, `UPDATE project SET lead_type = 'member', lead_id = $1 WHERE id = $2`, f.insider, f.projectID)
	for name, body := range map[string]map[string]any{
		"cleared":           {"lead_type": nil, "lead_id": nil},
		"id cleared":        {"lead_id": nil},
		"given to an agent": {"lead_type": "agent", "lead_id": agentID},
	} {
		// Both the lead and a workspace owner in the list are refused: it is
		// the project that needs a lead, whoever asks.
		for _, actor := range []string{f.insider, testUserID} {
			updateProjectAs(t, actor, f.projectID, body).Want(http.StatusBadRequest)
			if got := loadProjectLead(t, f.projectID); got != (projectLead{"member", f.insider}) {
				t.Fatalf("%s: refused update left lead = %+v", name, got)
			}
		}
	}
	// Handing it to another human stays possible.
	updateProjectAs(t, f.insider, f.projectID, map[string]any{"lead_type": "member", "lead_id": testUserID}).Want(http.StatusOK)
	if got := loadProjectLead(t, f.projectID); got != (projectLead{"member", testUserID}) {
		t.Fatalf("lead after handover = %+v", got)
	}

	// A workspace-visible project keeps an optional lead of either kind.
	open := dbfx.Project(t, f.marker+" open project")
	updateProjectAs(t, f.outsider, open, map[string]any{"lead_type": "agent", "lead_id": agentID}).Want(http.StatusOK)
	updateProjectAs(t, f.outsider, open, map[string]any{"lead_type": nil, "lead_id": nil}).Want(http.StatusOK)
	if got := loadProjectLead(t, open); got != (projectLead{}) {
		t.Fatalf("workspace-visible project lead = %+v, want none", got)
	}
}

func TestProjectTurningPrivateGetsHumanLead(t *testing.T) {
	marker := newPrivateProjectFixture(t).marker
	agentID := dbfx.Agent(t, marker+" lead agent", testRuntimeID)
	human := createPlainMember(t, marker+"-human-lead@multica.test")
	// A lead who has since left the workspace: the id points at nobody.
	departed := "00000000-0000-0000-0000-0000000d0001"

	for _, tc := range []struct {
		name     string
		lead     projectLead
		body     map[string]any
		wantCode int
		wantLead projectLead
	}{
		{"no lead: the caller leads", projectLead{}, nil, http.StatusOK, projectLead{"member", testUserID}},
		{"agent lead: the caller replaces it", projectLead{"agent", agentID}, nil, http.StatusOK, projectLead{"member", testUserID}},
		{"departed lead: the caller replaces them", projectLead{"member", departed}, nil, http.StatusOK, projectLead{"member", testUserID}},
		{"human lead kept", projectLead{"member", human}, nil, http.StatusOK, projectLead{"member", human}},
		{"lead named in the same request", projectLead{}, map[string]any{"lead_type": "member", "lead_id": human}, http.StatusOK, projectLead{"member", human}},
		{"agent named in the same request", projectLead{"member", human}, map[string]any{"lead_type": "agent", "lead_id": agentID}, http.StatusBadRequest, projectLead{"member", human}},
		{"lead cleared in the same request", projectLead{"member", human}, map[string]any{"lead_type": nil, "lead_id": nil}, http.StatusBadRequest, projectLead{"member", human}},
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
			updateProjectAs(t, testUserID, projectID, body).Want(tc.wantCode)
			if got := loadProjectLead(t, projectID); got != tc.wantLead {
				t.Fatalf("lead = %+v, want %+v", got, tc.wantLead)
			}
			if tc.wantCode != http.StatusOK {
				if n := dbfx.Count(t, `SELECT count(*) FROM project WHERE id = $1 AND visibility = 'private'`, projectID); n != 0 {
					t.Fatal("a refused update still made the project private")
				}
				return
			}
			members := projectMemberIDs(t, projectID)
			if !members[testUserID] || !members[tc.wantLead.leadID] || members[departed] {
				t.Fatalf("members = %v, want the caller and the lead", members)
			}
		})
	}
}

func TestPrivateProjectLeadHandedOverWhenLeadLeavesWorkspace(t *testing.T) {
	ctx := context.Background()
	marker := newPrivateProjectFixture(t).marker
	memberID := func(userID string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `SELECT id FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID).Scan(&id); err != nil {
			t.Fatalf("load member row: %v", err)
		}
		return id
	}
	leaver := createPlainMember(t, marker+"-leaver@multica.test")
	early := createPlainMember(t, marker+"-early@multica.test")
	late := createPlainMember(t, marker+"-late@multica.test")

	private := testutil.Cols{"visibility": projectaccess.VisibilityPrivate, "lead_type": "member", "lead_id": leaver}
	shared := dbfx.Project(t, marker+" shared", private)
	solo := dbfx.Project(t, marker+" solo", private)
	// Led by the leaver, but workspace-visible: the rule does not reach it.
	open := dbfx.Project(t, marker+" open", testutil.Cols{"lead_type": "member", "lead_id": leaver})
	// Private and led by someone else: untouched.
	other := dbfx.Project(t, marker+" other", testutil.Cols{"visibility": projectaccess.VisibilityPrivate, "lead_type": "member", "lead_id": early})
	// Created by someone who is still a member, alongside one who joined earlier.
	founded := dbfx.Project(t, marker+" founded", testutil.Cols{
		"visibility": projectaccess.VisibilityPrivate, "lead_type": "member", "lead_id": leaver,
		"creator_type": "member", "creator_id": late,
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_member WHERE project_id = ANY($1::uuid[])`, []string{shared, solo, open, other, founded})
	})
	addProjectMemberRow(t, solo, leaver)
	addProjectMemberRow(t, other, early)
	addProjectMemberRow(t, other, leaver)
	// Explicit join times: the handover goes to whoever has been in longest,
	// unless the project's creator is among those who stay.
	for _, projectID := range []string{shared, founded} {
		for i, userID := range []string{leaver, late, early} {
			addProjectMemberRow(t, projectID, userID)
			dbfx.Exec(t, `UPDATE project_member SET created_at = now() - make_interval(days => $3) WHERE project_id = $1 AND user_id = $2`, projectID, userID, i*10)
		}
	}

	result, err := testHandler.revokeAndRemoveMember(ctx, parseUUID(testWorkspaceID), parseUUID(leaver), parseUUID(memberID(leaver)), parseUUID(testUserID))
	if err != nil {
		t.Fatalf("revokeAndRemoveMember: %v", err)
	}

	if got := loadProjectLead(t, shared); got != (projectLead{"member", early}) {
		t.Errorf("shared project lead = %+v, want its longest-standing remaining member", got)
	}
	if members := projectMemberIDs(t, shared); members[leaver] || !members[early] || !members[late] {
		t.Errorf("shared project members = %v, want only the two who stayed", members)
	}
	if got := loadProjectLead(t, founded); got != (projectLead{"member", late}) {
		t.Errorf("founded project lead = %+v, want its creator, who is still a member", got)
	}
	// Nobody is left to take it: no lead, no members, still private. The
	// workspace owner recovers it (TestWorkspaceOwnerJoinsPrivateProject).
	if got := loadProjectLead(t, solo); got != (projectLead{}) {
		t.Errorf("solo project lead = %+v, want none", got)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM project WHERE id = $1 AND visibility = 'private'`, solo); n != 1 {
		t.Error("solo project is no longer private")
	}
	if got := loadProjectLead(t, open); got != (projectLead{"member", leaver}) {
		t.Errorf("workspace-visible project lead = %+v, want it left alone", got)
	}
	if got := loadProjectLead(t, other); got != (projectLead{"member", early}) {
		t.Errorf("project led by someone else = %+v, want it left alone", got)
	}

	handed := map[string]bool{}
	for _, p := range result.HandedOverProjects {
		handed[uuidToString(p.ID)] = true
	}
	if len(handed) != 3 || !handed[shared] || !handed[solo] || !handed[founded] {
		t.Errorf("handed-over projects = %v, want the three private projects the leaver led", handed)
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
