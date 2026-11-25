package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/connectedapps"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (f *appsFixture) overview(t *testing.T, userID string) (AppsOverviewResponse, map[string]json.RawMessage) {
	t.Helper()
	res := testutil.Call(t, f.h.GetAppsOverview, appsReqAs(t, userID, http.MethodGet, "/api/apps", nil)).Want(http.StatusOK)
	var typed AppsOverviewResponse
	var raw map[string]json.RawMessage
	res.JSON(&typed).JSON(&raw)
	return typed, raw
}

func (f *appsFixture) requestTeamApp(t *testing.T, userID string, body map[string]any) int {
	t.Helper()
	return testutil.Call(t, f.h.RequestTeamApp, appsReqAs(t, userID, http.MethodPost, "/api/apps/team/requests", body)).Code
}

func (f *appsFixture) dismissTeamRequests(t *testing.T, userID, slug string) int {
	t.Helper()
	return testutil.Call(t, f.h.DismissTeamAppRequests, appsReqAs(t, userID, http.MethodDelete, "/api/apps/team/requests?toolkit_slug="+slug, nil)).Code
}

func (f *appsFixture) teamAccount(t *testing.T, slug string) {
	t.Helper()
	dbfx.Insert(t, "workspace_composio_connection", testutil.Cols{
		"workspace_id": testWorkspaceID, "toolkit_slug": slug, "auth_config_id": "ac_" + slug,
		"connected_account_id": fmt.Sprintf("ca_team_%s_%d", slug, testSeq.Add(1)), "composio_user_id": "team", "connected_by": testUserID,
	})
}

// A member asking for a team account files the same request an agent does:
// the admins get one inbox item per app, and deciding it answers everyone.
func TestConnectedApps_TeamAccountRequests(t *testing.T) {
	f := newAppsFixture(t)

	_, raw := f.overview(t, f.member)
	if string(raw["team_requests"]) != "[]" {
		t.Fatalf("team_requests without asks = %s, want []", raw["team_requests"])
	}

	if code := f.requestTeamApp(t, f.member, map[string]any{"toolkit_slug": " Slack ", "agent_id": f.agentID}); code != http.StatusNoContent {
		t.Fatalf("member asks: %d", code)
	}
	if code := f.requestTeamApp(t, f.member, map[string]any{"toolkit_slug": "slack"}); code != http.StatusNoContent {
		t.Fatalf("member asks again: %d", code)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM app_action_request WHERE workspace_id=$1 AND approver_user_id=$2 AND status='pending'`, testWorkspaceID, f.member); n != 1 {
		t.Fatalf("pending asks after asking twice = %d, want 1", n)
	}
	if n := f.inboxCount(t, testUserID, connectedapps.NotifConnectRequested); n != 1 {
		t.Fatalf("admin notifications = %d, want 1 for a repeated ask", n)
	}
	if n := f.inboxCount(t, f.member, connectedapps.NotifConnectRequested); n != 0 {
		t.Fatalf("requester notified %d times", n)
	}
	var body, actor string
	dbfx.QueryRow(t, `SELECT body, actor_id FROM inbox_item WHERE workspace_id=$1 AND recipient_id=$2 AND type=$3`,
		testWorkspaceID, testUserID, connectedapps.NotifConnectRequested).Scan(&body, &actor)
	if !strings.Contains(body, "asks for a team Slack account") || actor != f.member {
		t.Fatalf("notification body %q actor %s", body, actor)
	}

	// An admin asking too is listed after the member and notifies nobody new.
	if code := f.requestTeamApp(t, testUserID, map[string]any{"toolkit_slug": "slack", "agent_id": f.agentID}); code != http.StatusNoContent {
		t.Fatalf("admin asks: %d", code)
	}
	if n := f.inboxCount(t, testUserID, connectedapps.NotifConnectRequested); n != 1 {
		t.Fatalf("admin notified again: %d", n)
	}
	got, _ := f.overview(t, f.member)
	if len(got.TeamRequests) != 1 {
		t.Fatalf("team requests = %+v", got.TeamRequests)
	}
	tr := got.TeamRequests[0]
	if tr.ToolkitSlug != "slack" || len(tr.RequestedBy) != 2 || tr.RequestedBy[0] != f.member || tr.RequestedBy[1] != testUserID ||
		len(tr.AskedBy) != 2 || len(tr.AgentIDs) != 1 || tr.AgentIDs[0] != f.agentID || tr.LastRequestedAt == "" {
		t.Fatalf("team request = %+v", tr)
	}

	for name, body := range map[string]map[string]any{
		"no slug":       {"toolkit_slug": " "},
		"bad agent":     {"toolkit_slug": "slack", "agent_id": "not-a-uuid"},
		"foreign agent": {"toolkit_slug": "slack", "agent_id": "00000000-0000-0000-0000-000000000001"},
	} {
		if code := f.requestTeamApp(t, f.member, body); code != http.StatusBadRequest {
			t.Fatalf("%s: %d, want 400", name, code)
		}
	}

	// A member's DELETE only withdraws their own ask.
	if code := f.dismissTeamRequests(t, f.member, "slack"); code != http.StatusNoContent {
		t.Fatalf("member withdraws: %d", code)
	}
	if got, _ := f.overview(t, f.member); len(got.TeamRequests) != 1 || len(got.TeamRequests[0].RequestedBy) != 1 || got.TeamRequests[0].RequestedBy[0] != testUserID {
		t.Fatalf("team requests after the member withdrew = %+v", got.TeamRequests)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM inbox_item WHERE workspace_id=$1 AND recipient_id=$2 AND type=$3 AND NOT archived`,
		testWorkspaceID, testUserID, connectedapps.NotifConnectRequested); n != 0 {
		t.Fatalf("admin still has %d open notifications for the withdrawn ask", n)
	}
	if code := f.requestTeamApp(t, f.member, map[string]any{"toolkit_slug": "slack"}); code != http.StatusNoContent {
		t.Fatalf("member asks again: %d", code)
	}
	if code := f.dismissTeamRequests(t, testUserID, "slack"); code != http.StatusNoContent {
		t.Fatalf("admin declines: %d", code)
	}
	if got, _ := f.overview(t, f.member); len(got.TeamRequests) != 0 {
		t.Fatalf("team requests after decline = %+v", got.TeamRequests)
	}
	if n := f.inboxCount(t, f.member, connectedapps.NotifActionResult); n != 1 {
		t.Fatalf("member told about the decline %d times, want 1", n)
	}
	if n := f.inboxCount(t, testUserID, connectedapps.NotifActionResult); n != 0 {
		t.Fatalf("admin told about their own decline %d times", n)
	}

	// Connecting the team account answers every ask for it.
	if code := f.requestTeamApp(t, f.member, map[string]any{"toolkit_slug": "notion"}); code != http.StatusNoContent {
		t.Fatalf("ask for notion: %d", code)
	}
	f.teamAccount(t, "notion")
	f.h.ConnectedApps.ResolveTeamConnectRequests(context.Background(), parseUUID(testWorkspaceID), parseUUID(testUserID), "notion")
	if n := dbfx.Count(t, `SELECT count(*) FROM app_action_request WHERE workspace_id=$1 AND toolkit_slug='notion' AND status='approved' AND decided_by=$2`, testWorkspaceID, testUserID); n != 1 {
		t.Fatalf("approved notion asks = %d, want 1", n)
	}
	if code := f.requestTeamApp(t, f.member, map[string]any{"toolkit_slug": "notion"}); code != http.StatusConflict {
		t.Fatalf("ask with an active team account: %d, want 409", code)
	}

	testutil.Call(t, f.h.UpdateAppSettings, appsReqAs(t, testUserID, http.MethodPut, "/api/apps/settings", map[string]any{"catalog_mode": "curated"})).Want(http.StatusOK)
	if code := f.requestTeamApp(t, f.member, map[string]any{"toolkit_slug": "some_unlisted_app"}); code != http.StatusForbidden {
		t.Fatalf("ask outside the catalog: %d, want 403", code)
	}

	// Leaving the workspace withdraws the member's asks.
	f.requestTeamApp(t, f.member, map[string]any{"toolkit_slug": "linear"})
	member, err := f.h.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID: parseUUID(f.member), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.h.revokeAndRemoveMember(context.Background(), member.WorkspaceID, member.UserID, member.ID, parseUUID(testUserID)); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM app_action_request WHERE workspace_id=$1 AND approver_user_id=$2 AND status='pending'`, testWorkspaceID, f.member); n != 0 {
		t.Fatalf("asks of a departed member = %d", n)
	}
}

// An agent missing a team account asks the admins, not the person who
// started the run; any admin connecting it resumes the run as that person.
func TestConnectedApps_AgentTeamConnectRequestGoesToAdmins(t *testing.T) {
	f := newAppsFixture(t)
	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "notion", "account_scope": "team", "level": "write"}}); w.Code != http.StatusOK {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}
	dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=$1, accountable_user_id=$1 WHERE id=$2`, f.member, f.taskID)

	_, text, isErr := f.gateway(t, f.token(t), "apps_request_connection", map[string]any{"app": "notion", "summary": "Read the spec"})
	if isErr || !strings.Contains(text, "workspace admins") {
		t.Fatalf("request connection = %s (error %v)", text, isErr)
	}
	if n := f.inboxCount(t, testUserID, connectedapps.NotifConnectRequested); n != 1 {
		t.Fatalf("admin notifications = %d, want 1", n)
	}
	if n := f.inboxCount(t, f.member, connectedapps.NotifConnectRequested); n != 0 {
		t.Fatalf("run starter notified %d times", n)
	}

	adminView := f.listRequests(t, testUserID)
	memberView := f.listRequests(t, f.member)
	if len(adminView) != 1 || !adminView[0].CanDecide || adminView[0].AccountScope != "team" {
		t.Fatalf("admin view = %+v", adminView)
	}
	if len(memberView) != 1 || memberView[0].CanDecide {
		t.Fatalf("member view = %+v", memberView)
	}
	if code := testutil.Call(t, f.h.ApproveAppRequest, withURLParam(appsReqAs(t, f.member, http.MethodPost, "/api/app-requests/"+adminView[0].ID+"/approve", nil), "id", adminView[0].ID)).Code; code == http.StatusOK {
		t.Fatal("member approved a team account request")
	}

	f.teamAccount(t, "notion")
	f.h.ConnectedApps.ResolveTeamConnectRequests(context.Background(), parseUUID(testWorkspaceID), parseUUID(testUserID), "notion")
	if got := f.requestStatus(t, adminView[0].ID); got != "approved" {
		t.Fatalf("request after the admin connected = %s", got)
	}
	var note, originator string
	dbfx.QueryRow(t, `SELECT handoff_note, originator_user_id FROM agent_task_queue
		WHERE agent_id=$1 AND issue_id=$2 AND status='queued' AND trigger_evidence_kind='app_action_request'`, f.agentID, f.issueID).Scan(&note, &originator)
	if !strings.Contains(note, "admin connected the team Notion account") || originator != f.member {
		t.Fatalf("resume run note %q originator %s, want the run starter", note, originator)
	}
}

// teamNotices counts a member's team account notifications for one app,
// open or archived.
func teamNotices(t *testing.T, recipient, slug string, archived bool) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM inbox_item WHERE workspace_id=$1 AND recipient_id=$2 AND type=$3
		AND details->>'toolkit_slug'=$4 AND archived=$5`, testWorkspaceID, recipient, connectedapps.NotifConnectRequested, slug, archived)
}

// A team account request goes to the admin it concerns: the run's starter
// when they are an admin, else the agent's owner when they are one, else
// every admin. Deciding it archives the other admins' notifications.
func TestConnectedApps_TeamConnectRequestRecipients(t *testing.T) {
	f := newAppsFixture(t)
	other := createTestUserAndMember(t, "admin")
	plain := createTestUserAndMember(t, "member")
	team := []map[string]string{}
	for _, slug := range []string{"notion", "slack", "linear"} {
		team = append(team, map[string]string{"toolkit_slug": slug, "account_scope": "team", "level": "write"})
	}
	if w := f.putAgentApps(t, testUserID, team); w.Code != http.StatusOK {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}
	ask := func(slug, originator, owner string) {
		t.Helper()
		dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=$1, accountable_user_id=$1 WHERE id=$2`, originator, f.taskID)
		dbfx.Exec(t, `UPDATE agent SET owner_id=$1 WHERE id=$2`, owner, f.agentID)
		if _, text, isErr := f.gateway(t, f.token(t), "apps_request_connection", map[string]any{"app": slug}); isErr {
			t.Fatalf("request %s: %s", slug, text)
		}
	}
	want := func(slug string, counts map[string]int) {
		t.Helper()
		for user, n := range counts {
			if got := teamNotices(t, user, slug, false); got != n {
				t.Fatalf("%s: open notifications for %s = %d, want %d", slug, user, got, n)
			}
		}
	}

	// The run's starter is an admin: only they are asked.
	ask("notion", testUserID, other)
	want("notion", map[string]int{testUserID: 1, other: 0, f.member: 0})

	// The starter is not an admin, the agent's owner is: only the owner.
	ask("slack", f.member, other)
	want("slack", map[string]int{testUserID: 0, other: 1, f.member: 0})

	// Neither is an admin: every admin.
	ask("linear", f.member, f.member)
	want("linear", map[string]int{testUserID: 1, other: 1, f.member: 0})

	// A later request for the same app reaches an admin nobody asked yet,
	// and does not repeat for one who already holds an open notification.
	if code := f.requestTeamApp(t, plain, map[string]any{"toolkit_slug": "slack"}); code != http.StatusNoContent {
		t.Fatalf("member asks for slack: %d", code)
	}
	want("slack", map[string]int{testUserID: 1, other: 1, plain: 0})

	// Connecting answers every request: the other admins' notifications are
	// archived, the connecting admin's stays where they acted on it.
	f.teamAccount(t, "linear")
	f.h.ConnectedApps.ResolveTeamConnectRequests(context.Background(), parseUUID(testWorkspaceID), parseUUID(testUserID), "linear")
	want("linear", map[string]int{testUserID: 1, other: 0})
	if n := teamNotices(t, other, "linear", true); n != 1 {
		t.Fatalf("archived linear notifications for the other admin = %d, want 1", n)
	}

	// So does declining.
	if code := f.dismissTeamRequests(t, other, "slack"); code != http.StatusNoContent {
		t.Fatalf("decline slack: %d", code)
	}
	want("slack", map[string]int{testUserID: 0, other: 1})
	if n := dbfx.Count(t, `SELECT count(*) FROM app_action_request WHERE workspace_id=$1 AND toolkit_slug='slack' AND status='pending'`, testWorkspaceID); n != 0 {
		t.Fatalf("pending slack requests after decline = %d", n)
	}
}

// waitingNotices counts a member's open notifications for the pending team
// account request approver is waiting on.
func waitingNotices(t *testing.T, recipient, approver string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM inbox_item i JOIN app_action_request r ON r.id = (i.details->>'request_id')::uuid
		WHERE i.workspace_id=$1 AND i.recipient_id=$2 AND i.type=$3 AND NOT i.archived
		  AND r.approver_user_id=$4 AND r.status='pending'`,
		testWorkspaceID, recipient, connectedapps.NotifConnectRequested, approver)
}

func pendingTeamAsk(t *testing.T, approver string) string {
	t.Helper()
	var id string
	dbfx.QueryRow(t, `SELECT id FROM app_action_request WHERE workspace_id=$1 AND approver_user_id=$2 AND status='pending'`,
		testWorkspaceID, approver).Scan(&id)
	return id
}

// One open notification stands for every request for an app, so when the
// request it names ends, however it ends, the admin who held it is told
// about the one still waiting.
func TestConnectedApps_TeamConnectRequestEndsWhileAnotherWaits(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		// end finishes the first of the two requests.
		end func(t *testing.T, f *appsFixture, first string)
		// openForAdmin counts the admin's open notifications afterwards: a
		// decided or expired one stays besides the new one.
		openForAdmin int
	}{
		"withdrawn by the person waiting": {func(t *testing.T, f *appsFixture, first string) {
			if _, err := f.h.ConnectedApps.Reject(ctx, parseUUID(testWorkspaceID), parseUUID(f.member), parseUUID(first), ""); err != nil {
				t.Fatalf("withdraw: %v", err)
			}
		}, 1},
		"ask withdrawn from the Apps page": {func(t *testing.T, f *appsFixture, _ string) {
			if code := f.dismissTeamRequests(t, f.member, "notion"); code != http.StatusNoContent {
				t.Fatalf("withdraw ask: %d", code)
			}
		}, 1},
		"declined alone by an admin": {func(t *testing.T, f *appsFixture, first string) {
			if _, err := f.h.ConnectedApps.Reject(ctx, parseUUID(testWorkspaceID), parseUUID(testUserID), parseUUID(first), ""); err != nil {
				t.Fatalf("decline: %v", err)
			}
		}, 2},
		"expired": {func(t *testing.T, f *appsFixture, first string) {
			dbfx.Exec(t, `UPDATE app_action_request SET expires_at = now() - interval '1 minute' WHERE id=$1`, first)
			f.h.ConnectedApps.Sweep(ctx)
		}, 2},
		"asker removed from the workspace": {func(t *testing.T, f *appsFixture, _ string) {
			member, err := f.h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
				UserID: parseUUID(f.member), WorkspaceID: parseUUID(testWorkspaceID),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.h.revokeAndRemoveMember(ctx, member.WorkspaceID, member.UserID, member.ID, parseUUID(testUserID)); err != nil {
				t.Fatalf("remove member: %v", err)
			}
		}, 1},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAppsFixture(t)
			second := createTestUserAndMember(t, "member")
			for _, asker := range []string{f.member, second} {
				if code := f.requestTeamApp(t, asker, map[string]any{"toolkit_slug": "notion"}); code != http.StatusNoContent {
					t.Fatalf("ask: %d", code)
				}
			}
			if n := teamNotices(t, testUserID, "notion", false); n != 1 {
				t.Fatalf("open notifications before = %d, want 1", n)
			}

			tc.end(t, f, pendingTeamAsk(t, f.member))

			if n := teamNotices(t, testUserID, "notion", false); n != tc.openForAdmin {
				t.Fatalf("open notifications after = %d, want %d", n, tc.openForAdmin)
			}
			if n := waitingNotices(t, testUserID, second); n != 1 {
				t.Fatalf("open notifications for the request still waiting = %d, want 1", n)
			}
		})
	}
}

// The notification passes only to admins who held one for the ended request
// and whom a waiting request concerns, once per app however many end.
func TestConnectedApps_TeamConnectNoticeHandOff(t *testing.T) {
	ctx := context.Background()
	f := newAppsFixture(t)
	other := createTestUserAndMember(t, "admin")
	second := createTestUserAndMember(t, "member")
	third := createTestUserAndMember(t, "member")
	ask := func(slug string, askers ...string) {
		t.Helper()
		for _, asker := range askers {
			if code := f.requestTeamApp(t, asker, map[string]any{"toolkit_slug": slug}); code != http.StatusNoContent {
				t.Fatalf("%s asks for %s: %d", asker, slug, code)
			}
		}
	}

	// An admin who dismissed theirs is not told again.
	ask("notion", f.member, second)
	dbfx.Exec(t, `UPDATE inbox_item SET archived = true WHERE workspace_id=$1 AND recipient_id=$2 AND type=$3`,
		testWorkspaceID, other, connectedapps.NotifConnectRequested)
	if code := f.dismissTeamRequests(t, f.member, "notion"); code != http.StatusNoContent {
		t.Fatalf("withdraw ask: %d", code)
	}
	if n := waitingNotices(t, testUserID, second); n != 1 {
		t.Fatalf("notion: holder's notifications for the request left = %d, want 1", n)
	}
	if n := teamNotices(t, other, "notion", false); n != 0 {
		t.Fatalf("notion: dismissing admin has %d open notifications, want 0", n)
	}

	// Requests expiring together hand over once.
	ask("linear", f.member, second, third)
	dbfx.Exec(t, `UPDATE app_action_request SET expires_at = now() - interval '1 minute'
		WHERE workspace_id=$1 AND toolkit_slug='linear' AND approver_user_id = ANY($2::uuid[])`,
		testWorkspaceID, []string{f.member, second})
	f.h.ConnectedApps.Sweep(ctx)
	if n := waitingNotices(t, testUserID, third); n != 1 {
		t.Fatalf("linear: notifications for the request left = %d, want 1", n)
	}
	if n := teamNotices(t, testUserID, "linear", false); n != 2 {
		t.Fatalf("linear: open notifications = %d, want 2: the expired one and the one left", n)
	}

	// A request that concerns one admin only is not handed to another: the
	// agent's run was started by an admin, so only they are asked.
	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "slack", "account_scope": "team", "level": "write"}}); w.Code != http.StatusOK {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}
	ask("slack", f.member)
	if _, text, isErr := f.gateway(t, f.token(t), "apps_request_connection", map[string]any{"app": "slack"}); isErr {
		t.Fatalf("agent asks for slack: %s", text)
	}
	if n := teamNotices(t, other, "slack", false); n != 1 {
		t.Fatalf("slack: other admin's open notifications before = %d, want 1", n)
	}
	if code := f.dismissTeamRequests(t, f.member, "slack"); code != http.StatusNoContent {
		t.Fatalf("withdraw ask: %d", code)
	}
	if n := waitingNotices(t, testUserID, testUserID); n != 1 {
		t.Fatalf("slack: run starter's notifications for the agent's request = %d, want 1", n)
	}
	if n := teamNotices(t, other, "slack", false); n != 0 {
		t.Fatalf("slack: admin the agent's request does not concern has %d open notifications, want 0", n)
	}
}

func TestConnectedApps_PersonalConnectionImpact(t *testing.T) {
	f := newAppsFixture(t)
	if w := f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal"}}); w.Code != http.StatusOK {
		t.Fatalf("attach: %d %s", w.Code, w.Body.String())
	}
	// A private agent of another member acts through whoever runs it, but the
	// caller cannot run it, so it is not theirs to see.
	privateAgent := dbfx.Agent(t, fmt.Sprintf("apps-private-%d", testSeq.Add(1)), handlerTestRuntimeID(t), testutil.Cols{
		"owner_id": f.member, "instructions": "",
		"custom_env": testutil.Raw("'{}'::jsonb"), "custom_args": testutil.Raw("'[]'::jsonb"),
	})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, privateAgent) })
	dbfx.Exec(t, `INSERT INTO agent_app (workspace_id, agent_id, toolkit_slug, account_scope, level) VALUES ($1, $2, 'gmail', 'personal', 'write')`, testWorkspaceID, privateAgent)

	var connectionID string
	dbfx.QueryRow(t, `SELECT id FROM user_composio_connection WHERE user_id=$1 AND toolkit_slug='gmail' AND status='active' ORDER BY created_at DESC LIMIT 1`, testUserID).Scan(&connectionID)
	impact := func(userID string) *testutil.Response {
		req := withURLParam(newRequestAs(userID, http.MethodGet, "/api/integrations/composio/connections/"+connectionID+"/impact", nil), "id", connectionID)
		return testutil.Call(t, f.h.GetComposioConnectionImpact, req)
	}

	var resp struct {
		Agents []ComposioImpactAgent `json:"agents"`
	}
	impact(testUserID).Want(http.StatusOK).JSON(&resp)
	var mine *ComposioImpactAgent
	for i := range resp.Agents {
		switch resp.Agents[i].ID {
		case f.agentID:
			mine = &resp.Agents[i]
		case privateAgent:
			t.Fatalf("impact names a private agent the caller cannot run: %+v", resp.Agents)
		}
	}
	if mine == nil || !mine.Running || mine.WorkspaceID != testWorkspaceID || mine.WorkspaceName == "" || mine.Name == "" {
		t.Fatalf("impact = %+v", resp.Agents)
	}

	dbfx.Exec(t, `UPDATE agent_task_queue SET status='completed', completed_at=now() WHERE id=$1`, f.taskID)
	impact(testUserID).Want(http.StatusOK).JSON(&resp)
	for _, a := range resp.Agents {
		if a.ID == f.agentID && a.Running {
			t.Fatalf("finished run still reported as running: %+v", a)
		}
	}

	impact(f.member).Want(http.StatusNotFound)
}

func TestConnectedApps_AgentAsksForNewApp(t *testing.T) {
	f := newAppsFixture(t)
	ctx := context.Background()

	_, text, isErr := f.gateway(t, f.token(t), "apps_request_connection", map[string]any{"app": "gmail", "summary": "Check my inbox"})
	if isErr || !strings.HasPrefix(text, "requested") || !strings.Contains(text, "add gmail") {
		t.Fatalf("ask for a new app = %s (error %v)", text, isErr)
	}
	reqs := f.listRequests(t, testUserID)
	if len(reqs) != 1 || reqs[0].Kind != "connect" || reqs[0].AccountScope != "personal" || reqs[0].ToolkitSlug != "gmail" || !reqs[0].AddsApp {
		t.Fatalf("requests = %+v", reqs)
	}
	var body string
	dbfx.QueryRow(t, `SELECT body FROM inbox_item WHERE workspace_id=$1 AND recipient_id=$2 AND type=$3`,
		testWorkspaceID, testUserID, connectedapps.NotifConnectRequested).Scan(&body)
	if !strings.Contains(body, "add Gmail") {
		t.Fatalf("notification body = %q", body)
	}

	approved := f.approve(t, reqs[0].ID, nil)
	if approved.Status != "approved" || approved.AddsApp {
		t.Fatalf("approved = %+v", approved)
	}
	var scope, level, createdBy string
	dbfx.QueryRow(t, `SELECT account_scope, level, created_by FROM agent_app WHERE agent_id=$1 AND toolkit_slug='gmail'`, f.agentID).Scan(&scope, &level, &createdBy)
	if scope != "personal" || level != connectedapps.LevelWrite || createdBy != testUserID {
		t.Fatalf("added app = %s/%s by %s", scope, level, createdBy)
	}
	var allowlist []string
	dbfx.QueryRow(t, `SELECT composio_toolkit_allowlist FROM agent WHERE id=$1`, f.agentID).Scan(&allowlist)
	if len(allowlist) != 1 || allowlist[0] != "gmail" {
		t.Fatalf("legacy allowlist = %v", allowlist)
	}
	var note string
	if err := testPool.QueryRow(ctx, `
		SELECT handoff_note FROM agent_task_queue
		WHERE agent_id = $1 AND issue_id = $2 AND status = 'queued' AND trigger_evidence_kind = 'app_action_request'
	`, f.agentID, f.issueID).Scan(&note); err != nil || !strings.Contains(note, "added Gmail to your apps") {
		t.Fatalf("resume note = %q, %v", note, err)
	}

	// An app the agent already has is a plain connection request.
	f.putAgentApps(t, testUserID, []map[string]string{{"toolkit_slug": "gmail", "account_scope": "personal"}, {"toolkit_slug": "notion", "account_scope": "personal"}})
	if _, text, isErr := f.gateway(t, f.token(t), "apps_request_connection", map[string]any{"app": "notion"}); isErr {
		t.Fatalf("connect notion = %s", text)
	}
	for _, r := range f.listRequests(t, testUserID) {
		if r.ToolkitSlug == "notion" && (r.Status != "pending" || r.AddsApp) {
			t.Fatalf("notion request = %+v", r)
		}
	}
}

func TestConnectedApps_AgentAsksForNewAppRefused(t *testing.T) {
	cases := map[string]struct {
		setup func(f *appsFixture)
		app   string
		want  string
	}{
		"run starter cannot edit the agent": {
			setup: func(f *appsFixture) {
				dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=$1, accountable_user_id=$1 WHERE id=$2`, f.member, f.taskID)
			},
			app: "gmail", want: "cannot add apps to you",
		},
		"no human originator": {
			setup: func(f *appsFixture) {
				dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=NULL, accountable_user_id=$1 WHERE id=$2`, testUserID, f.taskID)
			},
			app: "gmail", want: "no person triggered this run",
		},
		"unknown app": {app: "no_such_toolkit", want: "there is no app"},
		"outside the catalog": {
			setup: func(f *appsFixture) {
				dbfx.Exec(t, `INSERT INTO workspace_app_setting (workspace_id, catalog_mode, allowed_toolkits) VALUES ($1, 'custom', '{notion}')`, testWorkspaceID)
			},
			app: "gmail", want: "not available in this workspace",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newAppsFixture(t)
			if tc.setup != nil {
				tc.setup(f)
			}
			_, text, isErr := f.gateway(t, f.token(t), "apps_request_connection", map[string]any{"app": tc.app})
			if !isErr || !strings.Contains(text, tc.want) {
				t.Fatalf("response = %s (error %v), want %q", text, isErr, tc.want)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM app_action_request WHERE task_id=$1`, f.taskID); n != 0 {
				t.Fatalf("refused ask created %d requests", n)
			}
		})
	}
}

// Approving a request that adds an app re-checks that the approver may edit
// the agent, and withdraws the request when they no longer may.
func TestConnectedApps_AddAppApprovalFailsClosed(t *testing.T) {
	f := newAppsFixture(t)
	dbfx.Exec(t, `UPDATE agent SET owner_id=$1 WHERE id=$2`, f.member, f.agentID)
	dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=$1, accountable_user_id=$1 WHERE id=$2`, f.member, f.taskID)
	if _, text, isErr := f.gateway(t, f.token(t), "apps_request_connection", map[string]any{"app": "notion"}); isErr {
		t.Fatalf("owner asks: %s", text)
	}
	var requestID string
	dbfx.QueryRow(t, `SELECT id FROM app_action_request WHERE task_id=$1 AND kind='connect'`, f.taskID).Scan(&requestID)

	dbfx.Exec(t, `UPDATE agent SET owner_id=$1 WHERE id=$2`, testUserID, f.agentID)
	req := withURLParam(appsReqAs(t, f.member, http.MethodPost, "/api/app-requests/"+requestID+"/approve", nil), "id", requestID)
	testutil.Call(t, f.h.ApproveAppRequest, req).Want(http.StatusForbidden)
	if got := f.requestStatus(t, requestID); got != "cancelled" {
		t.Fatalf("request after a refused approval = %s, want cancelled", got)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_app WHERE agent_id=$1`, f.agentID); n != 0 {
		t.Fatalf("refused approval attached %d apps", n)
	}
}
