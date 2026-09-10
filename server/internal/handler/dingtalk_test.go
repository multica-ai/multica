package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/events"
	dingtalkintegration "github.com/multica-ai/multica/server/internal/integrations/dingtalk"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func wireDingTalkInstallService(t *testing.T) {
	t.Helper()
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	box, err := secretbox.New(make([]byte, secretbox.KeySize))
	if err != nil {
		t.Fatalf("secretbox.New: %v", err)
	}
	service, err := dingtalkintegration.NewInstallService(testHandler.Queries, testPool, box, nil)
	if err != nil {
		t.Fatalf("NewInstallService: %v", err)
	}
	previous := testHandler.DingTalkInstall
	testHandler.DingTalkInstall = service
	t.Cleanup(func() { testHandler.DingTalkInstall = previous })
}

type dingTalkAgentPermissionCase struct {
	name       string
	userID     string
	ownsAgent  bool
	denyStatus int
}

func createDingTalkNonMember(t *testing.T, email string) string {
	t.Helper()
	var userID string
	if err := testPool.QueryRow(context.Background(), `
INSERT INTO "user" (name, email) VALUES ('DingTalk non-member', $1) RETURNING id
`, email).Scan(&userID); err != nil {
		t.Fatalf("create DingTalk non-member: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return userID
}

func dingTalkAgentPermissionCases(t *testing.T, prefix, agentOwnerID, memberID string) []dingTalkAgentPermissionCase {
	t.Helper()
	adminID := createPermissionTestAdmin(t, prefix+"-admin@multica.test")
	nonMemberID := createDingTalkNonMember(t, prefix+"-non-member@multica.test")
	return []dingTalkAgentPermissionCase{
		{name: "workspace owner, not agent owner", userID: testUserID},
		{name: "workspace owner, agent owner", userID: testUserID, ownsAgent: true},
		{name: "workspace admin, not agent owner", userID: adminID},
		{name: "workspace admin, agent owner", userID: adminID, ownsAgent: true},
		{name: "workspace member, agent owner", userID: agentOwnerID, ownsAgent: true},
		{name: "workspace member, not agent owner", userID: memberID, denyStatus: http.StatusForbidden},
		{name: "non-member, agent owner", userID: nonMemberID, ownsAgent: true, denyStatus: http.StatusNotFound},
		{name: "non-member, not agent owner", userID: nonMemberID, denyStatus: http.StatusNotFound},
		{name: "unauthenticated", denyStatus: http.StatusUnauthorized},
	}
}

func setDingTalkAgentPermissionFixture(t *testing.T, agentID, permissionMode, ownerID string) {
	t.Helper()
	visibility := "private"
	if permissionMode == "public_to" {
		visibility = "workspace"
	}
	if _, err := testPool.Exec(context.Background(), `
UPDATE agent SET owner_id = $2, permission_mode = $3, visibility = $4 WHERE id = $1
`, agentID, ownerID, permissionMode, visibility); err != nil {
		t.Fatalf("update DingTalk permission fixture: %v", err)
	}
}

func setDingTalkAgentViewFixture(t *testing.T, agentID, accessModel, targetMemberID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `DELETE FROM agent_invocation_target WHERE agent_id = $1`, agentID); err != nil {
		t.Fatalf("clear DingTalk Agent targets: %v", err)
	}
	permissionMode := "private"
	visibility := "private"
	if accessModel != "private" {
		permissionMode = "public_to"
		visibility = "workspace"
	}
	if _, err := testPool.Exec(context.Background(), `
UPDATE agent SET permission_mode = $2, visibility = $3 WHERE id = $1
`, agentID, permissionMode, visibility); err != nil {
		t.Fatalf("update DingTalk Agent access model: %v", err)
	}
	switch accessModel {
	case "public_to_workspace":
		if _, err := testPool.Exec(context.Background(), `
INSERT INTO agent_invocation_target (agent_id, target_type, target_id) VALUES ($1, 'workspace', $2)
`, agentID, testWorkspaceID); err != nil {
			t.Fatalf("add DingTalk workspace target: %v", err)
		}
	case "public_to_member":
		if _, err := testPool.Exec(context.Background(), `
INSERT INTO agent_invocation_target (agent_id, target_type, target_id) VALUES ($1, 'member', $2)
`, agentID, targetMemberID); err != nil {
			t.Fatalf("add DingTalk member target: %v", err)
		}
	}
}

func TestListDingTalkGroupsForAgent_FollowsAgentViewPermissionMatrix(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID, ownerID, unrelatedMemberID := privateAgentTestFixture(t)
	adminID := createPermissionTestAdmin(t, "dingtalk-view-admin@multica.test")
	targetMemberID := createPermissionTestMember(t, "dingtalk-view-target@multica.test")
	nonMemberID := createDingTalkNonMember(t, "dingtalk-view-non-member@multica.test")
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_invocation_target WHERE agent_id = $1`, agentID)
	})

	previousInstall := testHandler.DingTalkInstall
	testHandler.DingTalkInstall = nil
	t.Cleanup(func() { testHandler.DingTalkInstall = previousInstall })

	actors := []struct {
		name      string
		userID    string
		ownsAgent bool
	}{
		{name: "workspace owner, not agent owner", userID: testUserID},
		{name: "workspace owner, agent owner", userID: testUserID, ownsAgent: true},
		{name: "workspace admin, not agent owner", userID: adminID},
		{name: "workspace admin, agent owner", userID: adminID, ownsAgent: true},
		{name: "workspace member, agent owner", userID: ownerID, ownsAgent: true},
		{name: "targeted member", userID: targetMemberID},
		{name: "unrelated member", userID: unrelatedMemberID},
		{name: "non-member", userID: nonMemberID},
		{name: "unauthenticated"},
	}
	accessModels := []struct {
		name string
		want map[string]int
	}{
		{
			name: "private",
			want: map[string]int{
				"workspace owner, not agent owner": http.StatusOK, "workspace owner, agent owner": http.StatusOK,
				"workspace admin, not agent owner": http.StatusOK, "workspace admin, agent owner": http.StatusOK,
				"workspace member, agent owner": http.StatusOK, "targeted member": http.StatusForbidden,
				"unrelated member": http.StatusForbidden, "non-member": http.StatusForbidden,
				"unauthenticated": http.StatusUnauthorized,
			},
		},
		{
			name: "public_to_workspace",
			want: map[string]int{
				"workspace owner, not agent owner": http.StatusOK, "workspace owner, agent owner": http.StatusOK,
				"workspace admin, not agent owner": http.StatusOK, "workspace admin, agent owner": http.StatusOK,
				"workspace member, agent owner": http.StatusOK, "targeted member": http.StatusOK,
				"unrelated member": http.StatusOK, "non-member": http.StatusForbidden,
				"unauthenticated": http.StatusUnauthorized,
			},
		},
		{
			name: "public_to_member",
			want: map[string]int{
				"workspace owner, not agent owner": http.StatusOK, "workspace owner, agent owner": http.StatusOK,
				"workspace admin, not agent owner": http.StatusOK, "workspace admin, agent owner": http.StatusOK,
				"workspace member, agent owner": http.StatusOK, "targeted member": http.StatusOK,
				"unrelated member": http.StatusForbidden, "non-member": http.StatusForbidden,
				"unauthenticated": http.StatusUnauthorized,
			},
		},
	}

	for _, accessModel := range accessModels {
		setDingTalkAgentViewFixture(t, agentID, accessModel.name, targetMemberID)
		for _, actor := range actors {
			t.Run(accessModel.name+"/"+actor.name, func(t *testing.T) {
				targetOwnerID := ownerID
				if actor.ownsAgent && actor.userID != "" {
					targetOwnerID = actor.userID
				}
				if _, err := testPool.Exec(context.Background(), `UPDATE agent SET owner_id = $2 WHERE id = $1`, agentID, targetOwnerID); err != nil {
					t.Fatalf("update DingTalk Agent owner: %v", err)
				}
				req := newRequestAs(actor.userID, http.MethodGet, "/api/agents/"+agentID+"/dingtalk/groups", nil)
				req = withURLParams(req, "id", agentID)
				w := httptest.NewRecorder()
				testHandler.ListDingTalkGroupsForAgent(w, req)
				if want := accessModel.want[actor.name]; w.Code != want {
					t.Fatalf("ListDingTalkGroupsForAgent: want %d, got %d: %s", want, w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestListDingTalkSettingsData_FollowsAgentViewPermissionMatrix(t *testing.T) {
	wireDingTalkInstallService(t)
	agentID, ownerID, unrelatedMemberID := privateAgentTestFixture(t)
	adminID := createPermissionTestAdmin(t, "dingtalk-settings-view-admin@multica.test")
	targetMemberID := createPermissionTestMember(t, "dingtalk-settings-view-target@multica.test")
	nonMemberID := createDingTalkNonMember(t, "dingtalk-settings-view-non-member@multica.test")
	installationID := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      testWorkspaceID,
		"agent_id":          agentID,
		"channel_type":      "dingtalk",
		"config":            []byte(`{"app_id":"dingtalk-settings-view","group_titles":{"cid-settings-view":"Settings group"},"group_bot_names":{"cid-settings-view":"Settings Bot"}}`),
		"installer_user_id": testUserID,
		"status":            "active",
	})
	if _, err := testPool.Exec(context.Background(), `
INSERT INTO dingtalk_group_presence (
  workspace_id, installation_id, conversation_id, conversation_title,
  bot_name, last_active_at, mention_count
) VALUES ($1, $2, 'cid-settings-view', 'Settings group', 'Settings Bot', $3, 1)
`, testWorkspaceID, installationID, time.Now().UTC()); err != nil {
		t.Fatalf("seed DingTalk presence: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM dingtalk_group_presence WHERE installation_id = $1`, installationID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM dingtalk_bot_identity WHERE installation_id = $1`, installationID)
	})

	actors := []struct {
		name      string
		userID    string
		ownsAgent bool
	}{
		{name: "workspace owner, not agent owner", userID: testUserID},
		{name: "workspace owner, agent owner", userID: testUserID, ownsAgent: true},
		{name: "workspace admin, not agent owner", userID: adminID},
		{name: "workspace admin, agent owner", userID: adminID, ownsAgent: true},
		{name: "workspace member, agent owner", userID: ownerID, ownsAgent: true},
		{name: "targeted member", userID: targetMemberID},
		{name: "unrelated member", userID: unrelatedMemberID},
		{name: "non-member", userID: nonMemberID},
		{name: "unauthenticated"},
	}
	accessModels := []struct {
		name    string
		visible map[string]bool
	}{
		{
			name: "private",
			visible: map[string]bool{
				"workspace owner, not agent owner": true, "workspace owner, agent owner": true,
				"workspace admin, not agent owner": true, "workspace admin, agent owner": true,
				"workspace member, agent owner": true,
			},
		},
		{
			name: "public_to_workspace",
			visible: map[string]bool{
				"workspace owner, not agent owner": true, "workspace owner, agent owner": true,
				"workspace admin, not agent owner": true, "workspace admin, agent owner": true,
				"workspace member, agent owner": true, "targeted member": true,
				"unrelated member": true,
			},
		},
		{
			name: "public_to_member",
			visible: map[string]bool{
				"workspace owner, not agent owner": true, "workspace owner, agent owner": true,
				"workspace admin, not agent owner": true, "workspace admin, agent owner": true,
				"workspace member, agent owner": true, "targeted member": true,
			},
		},
	}

	for _, accessModel := range accessModels {
		setDingTalkAgentViewFixture(t, agentID, accessModel.name, targetMemberID)
		for _, actor := range actors {
			t.Run(accessModel.name+"/"+actor.name, func(t *testing.T) {
				targetOwnerID := ownerID
				if actor.ownsAgent && actor.userID != "" {
					targetOwnerID = actor.userID
				}
				if _, err := testPool.Exec(context.Background(), `UPDATE agent SET owner_id = $2 WHERE id = $1`, agentID, targetOwnerID); err != nil {
					t.Fatalf("update DingTalk Agent owner: %v", err)
				}

				installReq := newRequestAs(actor.userID, http.MethodGet,
					"/api/workspaces/"+testWorkspaceID+"/dingtalk/installations", nil)
				installReq = withURLParams(installReq, "id", testWorkspaceID)
				installRec := httptest.NewRecorder()
				testHandler.ListDingTalkInstallations(installRec, installReq)

				groupReq := newRequestAs(actor.userID, http.MethodGet,
					"/api/workspaces/"+testWorkspaceID+"/dingtalk/groups", nil)
				groupReq = withURLParams(groupReq, "id", testWorkspaceID)
				groupRec := httptest.NewRecorder()
				testHandler.ListDingTalkGroups(groupRec, groupReq)

				wantStatus := http.StatusOK
				if actor.userID == "" {
					wantStatus = http.StatusUnauthorized
				} else if actor.name == "non-member" {
					wantStatus = http.StatusNotFound
				}
				if installRec.Code != wantStatus || groupRec.Code != wantStatus {
					t.Fatalf("Settings status: installations=%d groups=%d, want %d", installRec.Code, groupRec.Code, wantStatus)
				}
				if wantStatus != http.StatusOK {
					return
				}

				var installations struct {
					Installations []DingTalkInstallationResponse `json:"installations"`
				}
				if err := json.Unmarshal(installRec.Body.Bytes(), &installations); err != nil {
					t.Fatal(err)
				}
				var groups ListDingTalkGroupsResponse
				if err := json.Unmarshal(groupRec.Body.Bytes(), &groups); err != nil {
					t.Fatal(err)
				}
				wantCount := 0
				if accessModel.visible[actor.name] {
					wantCount = 1
				}
				if len(installations.Installations) != wantCount || len(groups.Groups) != wantCount {
					t.Fatalf("Settings visibility: installations=%+v groups=%+v, want count %d", installations.Installations, groups.Groups, wantCount)
				}
				if wantCount == 1 && installations.Installations[0].ID != installationID {
					t.Fatalf("installation id = %s, want %s", installations.Installations[0].ID, installationID)
				}
			})
		}
	}
}

func TestListDingTalkGroupsInactivePaginationAuthorizesInstallationBeforeCursor(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	wireDingTalkInstallService(t)
	agentID, agentOwnerID, unrelatedMemberID := privateAgentTestFixture(t)
	installationID := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      testWorkspaceID,
		"agent_id":          agentID,
		"channel_type":      "dingtalk",
		"config":            []byte(`{"app_id":"dingtalk-private-inactive-pagination"}`),
		"installer_user_id": testUserID,
		"status":            "active",
	})
	for _, group := range []struct {
		conversationID    string
		conversationTitle string
	}{
		{conversationID: "cid-private-inactive-a", conversationTitle: "Private inactive A"},
		{conversationID: "cid-private-inactive-b", conversationTitle: "Private inactive B"},
	} {
		dbfx.InsertNoID(t, "dingtalk_group_presence", testutil.Cols{
			"workspace_id":       testWorkspaceID,
			"installation_id":    installationID,
			"conversation_id":    group.conversationID,
			"conversation_title": group.conversationTitle,
			"last_active_at":     testutil.Raw("now() - interval '91 days'"),
			"mention_count":      1,
		}, "installation_id = $1 AND conversation_id = $2", installationID, group.conversationID)
	}
	otherAgentID := dbfx.Agent(t, "dingtalk-private-pagination-other-agent", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id": testUserID,
	})
	otherInstallationID := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      testWorkspaceID,
		"agent_id":          otherAgentID,
		"channel_type":      "dingtalk",
		"config":            []byte(`{"app_id":"dingtalk-private-pagination-other-installation"}`),
		"installer_user_id": testUserID,
		"status":            "active",
	})
	for _, conversationID := range []string{"cid-private-other-a", "cid-private-other-b"} {
		dbfx.InsertNoID(t, "dingtalk_group_presence", testutil.Cols{
			"workspace_id":       testWorkspaceID,
			"installation_id":    otherInstallationID,
			"conversation_id":    conversationID,
			"conversation_title": conversationID,
			"last_active_at":     testutil.Raw("now() - interval '91 days'"),
			"mention_count":      1,
		}, "installation_id = $1 AND conversation_id = $2", otherInstallationID, conversationID)
	}
	revokedAgentID := dbfx.Agent(t, "dingtalk-private-pagination-revoked-agent", handlerTestRuntimeID(t), testutil.Cols{
		"owner_id": agentOwnerID,
	})
	revokedInstallationID := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      testWorkspaceID,
		"agent_id":          revokedAgentID,
		"channel_type":      "dingtalk",
		"config":            []byte(`{"app_id":"dingtalk-private-pagination-revoked"}`),
		"installer_user_id": testUserID,
		"status":            "revoked",
	})

	request := func(userID, requestedInstallationID, suffix string) *http.Request {
		path := "/api/workspaces/" + testWorkspaceID + "/dingtalk/groups?activity=inactive&installation_id=" + requestedInstallationID + suffix
		return testutil.WithURLParams(newRequestAs(userID, http.MethodGet, path, nil), "id", testWorkspaceID)
	}

	inaccessible := testutil.Call(t, testHandler.ListDingTalkGroups,
		request(unrelatedMemberID, installationID, "&limit=1&offset=0")).Want(http.StatusOK)
	var inaccessibleBody ListDingTalkGroupsResponse
	inaccessible.JSON(&inaccessibleBody)
	if len(inaccessibleBody.Groups) != 0 || inaccessibleBody.NextOffset != nil {
		t.Fatalf("inaccessible private installation leaked pagination: %+v", inaccessibleBody)
	}
	if _, leaked := inaccessibleBody.InactiveGroupCounts[installationID]; leaked {
		t.Fatalf("inaccessible private installation leaked inactive count: %+v", inaccessibleBody.InactiveGroupCounts)
	}
	if _, leaked := inaccessibleBody.BotIdentities[installationID]; leaked {
		t.Fatalf("inaccessible private installation leaked bot identity: %+v", inaccessibleBody.BotIdentities)
	}
	if _, leaked := inaccessible.Map()["next_offset"]; leaked {
		t.Fatalf("inaccessible private installation serialized next_offset: %s", inaccessible.Text())
	}

	const missingInstallationID = "d1473000-0000-4000-8000-000000000099"
	missing := testutil.Call(t, testHandler.ListDingTalkGroups,
		request(unrelatedMemberID, missingInstallationID, "&limit=1&offset=0")).Want(http.StatusOK)
	if inaccessible.Text() != missing.Text() {
		t.Fatalf("inaccessible and nonexistent installations must be indistinguishable:\ninaccessible: %s\nnonexistent: %s",
			inaccessible.Text(), missing.Text())
	}

	firstPage := testutil.Decode[ListDingTalkGroupsResponse](t, testHandler.ListDingTalkGroups,
		request(agentOwnerID, installationID, "&limit=1&offset=0"), http.StatusOK)
	if len(firstPage.Groups) != 1 || firstPage.NextOffset == nil || *firstPage.NextOffset != 1 {
		t.Fatalf("visible private installation first page = %+v, want one group and next_offset=1", firstPage)
	}
	if firstPage.InactiveGroupCounts[installationID] != 2 {
		t.Fatalf("visible private installation inactive counts = %+v, want %s=2",
			firstPage.InactiveGroupCounts, installationID)
	}

	secondPage := testutil.Decode[ListDingTalkGroupsResponse](t, testHandler.ListDingTalkGroups,
		request(agentOwnerID, installationID, "&limit=1&offset=1"), http.StatusOK)
	if len(secondPage.Groups) != 1 || secondPage.NextOffset != nil {
		t.Fatalf("visible private installation second page = %+v, want one terminal group", secondPage)
	}

	visibleMissing := testutil.Call(t, testHandler.ListDingTalkGroups,
		request(agentOwnerID, missingInstallationID, "&limit=1&offset=0")).Want(http.StatusOK)
	revoked := testutil.Call(t, testHandler.ListDingTalkGroups,
		request(agentOwnerID, revokedInstallationID, "&limit=1&offset=0")).Want(http.StatusOK)
	if revoked.Text() != visibleMissing.Text() {
		t.Fatalf("revoked and nonexistent installations must be indistinguishable:\nrevoked: %s\nnonexistent: %s",
			revoked.Text(), visibleMissing.Text())
	}

	agentRequest := func(requestedInstallationID string) *http.Request {
		path := "/api/agents/" + agentID + "/dingtalk/groups?activity=inactive&installation_id=" + requestedInstallationID + "&limit=1&offset=0"
		return testutil.WithURLParams(newRequestAs(agentOwnerID, http.MethodGet, path, nil), "id", agentID)
	}
	agentPage := testutil.Decode[ListDingTalkGroupsResponse](t, testHandler.ListDingTalkGroupsForAgent,
		agentRequest(installationID), http.StatusOK)
	if len(agentPage.Groups) != 1 || agentPage.NextOffset == nil || *agentPage.NextOffset != 1 {
		t.Fatalf("Agent-scoped own installation page = %+v, want one group and next_offset=1", agentPage)
	}
	wrongAgentInstallation := testutil.Call(t, testHandler.ListDingTalkGroupsForAgent,
		agentRequest(otherInstallationID)).Want(http.StatusOK)
	missingAgentInstallation := testutil.Call(t, testHandler.ListDingTalkGroupsForAgent,
		agentRequest(missingInstallationID)).Want(http.StatusOK)
	if wrongAgentInstallation.Text() != missingAgentInstallation.Text() {
		t.Fatalf("another Agent's installation and a nonexistent installation must be indistinguishable:\nother Agent: %s\nnonexistent: %s",
			wrongAgentInstallation.Text(), missingAgentInstallation.Text())
	}
}

func TestListDingTalkInstallations_OrphanIsAdminOnlyAndMarkedUnavailable(t *testing.T) {
	wireDingTalkInstallService(t)
	agentID, _, memberID := privateAgentTestFixture(t)
	installationID := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      testWorkspaceID,
		"agent_id":          agentID,
		"channel_type":      "dingtalk",
		"config":            []byte(`{"app_id":"dingtalk-orphan"}`),
		"installer_user_id": testUserID,
		"status":            "active",
	})
	if _, err := testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID); err != nil {
		t.Fatalf("delete DingTalk Agent fixture: %v", err)
	}

	list := func(userID string) []DingTalkInstallationResponse {
		req := newRequestAs(userID, http.MethodGet,
			"/api/workspaces/"+testWorkspaceID+"/dingtalk/installations", nil)
		req = withURLParams(req, "id", testWorkspaceID)
		rec := httptest.NewRecorder()
		testHandler.ListDingTalkInstallations(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("ListDingTalkInstallations status = %d: %s", rec.Code, rec.Body.String())
		}
		var response struct {
			Installations []DingTalkInstallationResponse `json:"installations"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response.Installations
	}

	adminRows := list(testUserID)
	found := false
	for _, row := range adminRows {
		if row.ID == installationID {
			found = true
			if row.AgentAvailable {
				t.Fatal("orphaned installation was marked as having an available Agent")
			}
		}
	}
	if !found {
		t.Fatal("workspace owner could not see orphaned installation for cleanup")
	}
	for _, row := range list(memberID) {
		if row.ID == installationID {
			t.Fatal("ordinary member could see orphaned installation")
		}
	}
}

func TestRegisterDingTalkBYO_AuthorizesAgentOwnerAndAdmins(t *testing.T) {
	wireDingTalkInstallService(t)
	agentID, ownerID, memberID := privateAgentTestFixture(t)
	cases := dingTalkAgentPermissionCases(t, "dingtalk-register", ownerID, memberID)

	register := func(userID string) *httptest.ResponseRecorder {
		req := newRequestAs(userID, http.MethodPost,
			"/api/workspaces/"+testWorkspaceID+"/dingtalk/install/byo?agent_id="+agentID, nil)
		req = withURLParams(req, "id", testWorkspaceID)
		w := httptest.NewRecorder()
		testHandler.RegisterDingTalkBYO(w, req)
		return w
	}

	for _, permissionMode := range []string{"private", "public_to"} {
		for _, tc := range cases {
			t.Run(permissionMode+"/"+tc.name, func(t *testing.T) {
				targetOwnerID := ownerID
				if tc.ownsAgent && tc.userID != "" {
					targetOwnerID = tc.userID
				}
				setDingTalkAgentPermissionFixture(t, agentID, permissionMode, targetOwnerID)
				want := tc.denyStatus
				if want == 0 {
					// Authorized requests reach body validation; the empty body is
					// deliberate so this test never contacts DingTalk.
					want = http.StatusBadRequest
				}
				w := register(tc.userID)
				if w.Code != want {
					t.Fatalf("RegisterDingTalkBYO: want %d, got %d: %s", want, w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestForgetDingTalkGroup_AdminOnlyAndKeepsInstallation(t *testing.T) {
	wireDingTalkInstallService(t)
	agentID, _, memberID := privateAgentTestFixture(t)
	installationID := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      testWorkspaceID,
		"agent_id":          agentID,
		"channel_type":      "dingtalk",
		"config":            []byte(`{"app_id":"dingtalk-forget"}`),
		"installer_user_id": testUserID,
		"status":            "active",
	})
	if _, err := testPool.Exec(context.Background(), `
INSERT INTO dingtalk_group_presence (
  workspace_id, installation_id, conversation_id, conversation_title
) VALUES ($1, $2, 'cid/forget', 'Forget me')
`, testWorkspaceID, installationID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `
INSERT INTO dingtalk_bot_identity (
  workspace_id, installation_id, bot_name
) VALUES ($1, $2, 'Persistent Bot')
`, testWorkspaceID, installationID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM dingtalk_group_presence WHERE installation_id = $1`, installationID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM dingtalk_bot_identity WHERE installation_id = $1`, installationID)
	})

	forget := func(userID string) int {
		router := chi.NewRouter()
		router.Delete(
			"/api/workspaces/{id}/dingtalk/installations/{installationId}/groups/{conversationId}",
			testHandler.ForgetDingTalkGroup,
		)
		req := newRequestAs(userID, http.MethodDelete,
			"/api/workspaces/"+testWorkspaceID+"/dingtalk/installations/"+installationID+"/groups/cid%2Fforget", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := forget(memberID); got != http.StatusForbidden {
		t.Fatalf("member forget status = %d, want 403", got)
	}
	if got := forget(testUserID); got != http.StatusNoContent {
		t.Fatalf("owner forget status = %d, want 204", got)
	}
	var presenceCount, identityCount, installationCount int
	var botName string
	if err := testPool.QueryRow(context.Background(), `
SELECT
  (SELECT count(*) FROM dingtalk_group_presence WHERE installation_id = $1),
  (SELECT count(*) FROM dingtalk_bot_identity WHERE installation_id = $1),
  (SELECT bot_name FROM dingtalk_bot_identity WHERE installation_id = $1),
  (SELECT count(*) FROM channel_installation WHERE id = $1)
`, installationID).Scan(&presenceCount, &identityCount, &botName, &installationCount); err != nil {
		t.Fatal(err)
	}
	if presenceCount != 0 || identityCount != 1 || botName != "Persistent Bot" || installationCount != 1 {
		t.Fatalf("forget state = presence %d identity %d/%q installation %d", presenceCount, identityCount, botName, installationCount)
	}

	listReq := newRequestAs(testUserID, http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/dingtalk/groups", nil)
	listReq = withURLParams(listReq, "id", testWorkspaceID)
	listRec := httptest.NewRecorder()
	testHandler.ListDingTalkGroups(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list groups after forget status = %d: %s", listRec.Code, listRec.Body.String())
	}
	var listing ListDingTalkGroupsResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	forgottenGroupPresent := false
	for _, group := range listing.Groups {
		for _, bot := range group.Bots {
			forgottenGroupPresent = forgottenGroupPresent || bot.InstallationID == installationID
		}
	}
	if forgottenGroupPresent || listing.BotIdentities[installationID].BotName != "Persistent Bot" {
		t.Fatalf("listing after forget = groups %+v identities %+v", listing.Groups, listing.BotIdentities)
	}
}

func TestRevokeDingTalkInstallation_AuthorizesAgentOwnerAndAdmins(t *testing.T) {
	wireDingTalkInstallService(t)
	agentID, ownerID, memberID := privateAgentTestFixture(t)
	cases := dingTalkAgentPermissionCases(t, "dingtalk-revoke", ownerID, memberID)

	seedInstallation := func(agentID, appID string) string {
		if _, err := testPool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE workspace_id = $1 AND agent_id = $2 AND channel_type = 'dingtalk'`,
			testWorkspaceID, agentID); err != nil {
			t.Fatalf("clear prior DingTalk installation: %v", err)
		}
		return dbfx.Insert(t, "channel_installation", testutil.Cols{
			"workspace_id":      testWorkspaceID,
			"agent_id":          agentID,
			"channel_type":      "dingtalk",
			"config":            []byte(`{"app_id":"` + appID + `"}`),
			"installer_user_id": testUserID,
			"status":            "active",
		})
	}
	revoke := func(userID, installationID string) int {
		req := newRequestAs(userID, http.MethodDelete,
			"/api/workspaces/"+testWorkspaceID+"/dingtalk/installations/"+installationID, nil)
		req = withURLParams(req, "id", testWorkspaceID, "installationId", installationID)
		w := httptest.NewRecorder()
		testHandler.RevokeDingTalkInstallation(w, req)
		return w.Code
	}

	for _, permissionMode := range []string{"private", "public_to"} {
		for _, tc := range cases {
			t.Run(permissionMode+"/"+tc.name, func(t *testing.T) {
				targetOwnerID := ownerID
				if tc.ownsAgent && tc.userID != "" {
					targetOwnerID = tc.userID
				}
				setDingTalkAgentPermissionFixture(t, agentID, permissionMode, targetOwnerID)
				want := tc.denyStatus
				if want == 0 {
					want = http.StatusNoContent
				}
				if code := revoke(tc.userID, seedInstallation(agentID, "dingtalk-permission-matrix")); code != want {
					t.Fatalf("RevokeDingTalkInstallation: want %d, got %d", want, code)
				}
			})
		}
	}
}

func TestRevokeDingTalkInstallation_OrphanCleanableByAdminNotMember(t *testing.T) {
	wireDingTalkInstallService(t)
	_, _, memberID := privateAgentTestFixture(t)
	adminID := createPermissionTestAdmin(t, "dingtalk-orphan-admin@multica.test")
	nonMemberID := createDingTalkNonMember(t, "dingtalk-orphan-non-member@multica.test")
	const orphanAgentID = "d1473000-0000-4000-8000-0000000000aa"

	seedOrphan := func(appID string) string {
		if _, err := testPool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE workspace_id = $1 AND agent_id = $2 AND channel_type = 'dingtalk'`,
			testWorkspaceID, orphanAgentID); err != nil {
			t.Fatalf("clear prior orphan DingTalk installation: %v", err)
		}
		return dbfx.Insert(t, "channel_installation", testutil.Cols{
			"workspace_id":      testWorkspaceID,
			"agent_id":          orphanAgentID,
			"channel_type":      "dingtalk",
			"config":            []byte(`{"app_id":"` + appID + `"}`),
			"installer_user_id": testUserID,
			"status":            "active",
		})
	}
	revoke := func(userID, installationID string) int {
		req := newRequestAs(userID, http.MethodDelete,
			"/api/workspaces/"+testWorkspaceID+"/dingtalk/installations/"+installationID, nil)
		req = withURLParams(req, "id", testWorkspaceID, "installationId", installationID)
		w := httptest.NewRecorder()
		testHandler.RevokeDingTalkInstallation(w, req)
		return w.Code
	}

	for _, tc := range []struct {
		name   string
		userID string
		want   int
	}{
		{name: "workspace owner", userID: testUserID, want: http.StatusNoContent},
		{name: "workspace admin", userID: adminID, want: http.StatusNoContent},
		{name: "workspace member", userID: memberID, want: http.StatusForbidden},
		{name: "non-member", userID: nonMemberID, want: http.StatusNotFound},
		{name: "unauthenticated", want: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := revoke(tc.userID, seedOrphan("dingtalk-orphan-permission-matrix")); code != tc.want {
				t.Fatalf("orphan revoke: want %d, got %d", tc.want, code)
			}
		})
	}
}

func TestPublishDingTalkInstallationCreated(t *testing.T) {
	bus := events.New()
	h := &Handler{Bus: bus}

	const (
		wsID   = "11111111-1111-1111-1111-111111111111"
		instID = "22222222-2222-2222-2222-222222222222"
	)

	var got events.Event
	fired := 0
	bus.Subscribe(protocol.EventDingTalkInstallationCreated, func(e events.Event) {
		got = e
		fired++
	})

	h.publishDingTalkInstallationCreated(db.ChannelInstallation{
		ID:          parseUUID(instID),
		WorkspaceID: parseUUID(wsID),
	}, "user-1")

	if fired != 1 {
		t.Fatalf("expected dingtalk_installation:created published once, got %d", fired)
	}
	if got.WorkspaceID != wsID || got.ActorType != "user" || got.ActorID != "user-1" {
		t.Errorf("event envelope = %+v", got)
	}
	payload, ok := got.Payload.(map[string]any)
	if !ok || payload["id"] != instID {
		t.Errorf("payload = %v, want installation id %s", got.Payload, instID)
	}
}

func TestRedeemDingTalkBindingTokenPublishesAccountBindingUpdateAfterCommit(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	agentID := createHandlerTestAgent(t, "DingTalk binding event Agent", nil)
	installationID := dbfx.Insert(t, "channel_installation", testutil.Cols{
		"workspace_id":      testWorkspaceID,
		"agent_id":          agentID,
		"channel_type":      "dingtalk",
		"config":            []byte(`{"app_id":"handler-binding-event"}`),
		"installer_user_id": testUserID,
	})
	dbfx.Cleanup(t, `DELETE FROM channel_user_binding WHERE installation_id = $1`, installationID)
	dbfx.Cleanup(t, `DELETE FROM channel_binding_token WHERE installation_id = $1`, installationID)

	bindingService := dingtalkintegration.NewBindingTokenService(testHandler.Queries, testPool)
	token, err := bindingService.Mint(
		context.Background(),
		parseUUID(testWorkspaceID),
		parseUUID(installationID),
		"staff-binding-event",
	)
	if err != nil {
		t.Fatalf("mint DingTalk binding token: %v", err)
	}

	bus := events.New()
	h := &Handler{
		Bus:                   bus,
		DingTalkBindingTokens: bindingService,
	}

	var (
		published      events.Event
		eventCount     int
		bindingAtEvent db.ChannelUserBinding
		queryErr       error
	)
	bus.Subscribe(protocol.EventDingTalkAccountBindingUpdated, func(event events.Event) {
		eventCount++
		published = event
		// The synchronous subscriber reads through a separate pool connection,
		// proving the binding transaction committed before the event fired.
		bindingAtEvent, queryErr = testHandler.Queries.GetChannelUserBindingByUserID(
			context.Background(),
			db.GetChannelUserBindingByUserIDParams{
				InstallationID: parseUUID(installationID),
				ChannelUserID:  "staff-binding-event",
			},
		)
	})

	recorder := httptest.NewRecorder()
	h.RedeemDingTalkBindingToken(recorder, newRequest(
		http.MethodPost,
		"/api/dingtalk/binding/redeem",
		RedeemDingTalkBindingTokenRequest{Token: token.Raw},
	))

	if recorder.Code != http.StatusOK {
		t.Fatalf("redeem status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if eventCount != 1 {
		t.Fatalf("binding update event count = %d, want 1", eventCount)
	}
	if queryErr != nil {
		t.Fatalf("binding was not visible when event fired: %v", queryErr)
	}
	if bindingAtEvent.MulticaUserID != parseUUID(testUserID) {
		t.Errorf("binding user = %v, want %s", bindingAtEvent.MulticaUserID, testUserID)
	}
	if published.WorkspaceID != testWorkspaceID || published.ActorType != "user" || published.ActorID != testUserID {
		t.Errorf("event envelope = %+v", published)
	}
	payload, ok := published.Payload.(map[string]any)
	if !ok || payload["id"] != installationID {
		t.Errorf("payload = %v, want installation id %s", published.Payload, installationID)
	}
}

func TestListDingTalkGroupsAggregatesConnectedBots(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler database unavailable")
	}
	const (
		installOne = "d1473000-0000-4000-8000-000000000011"
		installTwo = "d1473000-0000-4000-8000-000000000012"
		agentOne   = "d1473000-0000-4000-8000-000000000021"
		agentTwo   = "d1473000-0000-4000-8000-000000000022"
		sessionID  = "d1473000-0000-4000-8000-000000000031"
	)
	previousInstall := testHandler.DingTalkInstall
	testHandler.DingTalkInstall = &dingtalkintegration.InstallService{}
	t.Cleanup(func() { testHandler.DingTalkInstall = previousInstall })
	clean := func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM dingtalk_group_presence WHERE installation_id = ANY($1::uuid[])`, []string{installOne, installTwo})
		_, _ = testPool.Exec(context.Background(), `DELETE FROM dingtalk_bot_identity WHERE installation_id = ANY($1::uuid[])`, []string{installOne, installTwo})
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_chat_session_binding WHERE chat_session_id = $1`, sessionID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_installation WHERE id = ANY($1::uuid[])`, []string{installOne, installTwo})
	}
	clean()
	t.Cleanup(clean)
	for _, fixture := range []struct {
		id      string
		agentID string
		appID   string
		name    string
		title   string
		status  string
	}{
		{id: installOne, agentID: agentOne, appID: "handler-group-one", name: "Release Bot", title: "", status: "active"},
		{id: installTwo, agentID: agentTwo, appID: "handler-group-two", name: "Support Bot", title: "Platform", status: "active"},
	} {
		if _, err := testPool.Exec(context.Background(), `
INSERT INTO channel_installation (
  id, workspace_id, agent_id, channel_type, config, installer_user_id, status
) VALUES (
  $1, $2, $3, 'dingtalk',
  jsonb_build_object('app_id', $4::text),
  $5, $6
)
`, fixture.id, testWorkspaceID, fixture.agentID, fixture.appID, testUserID, fixture.status); err != nil {
			t.Fatalf("seed group bot: %v", err)
		}
		if _, err := testPool.Exec(context.Background(), `
INSERT INTO dingtalk_group_presence (
  workspace_id, installation_id, conversation_id, conversation_title,
  bot_name, last_active_at, mention_count
) VALUES ($1, $2, 'cid-shared', $3, $4, now(), $5)
`, testWorkspaceID, fixture.id, fixture.title, fixture.name, map[string]int64{installOne: 2, installTwo: 1}[fixture.id]); err != nil {
			t.Fatalf("seed group presence: %v", err)
		}
	}
	if _, err := testPool.Exec(context.Background(), `
INSERT INTO dingtalk_group_presence (
  workspace_id, installation_id, conversation_id, conversation_title
) VALUES
  ($1, $2, 'cid-z', 'Platform'),
  ($1, $2, 'cid-untitled', '')
`, testWorkspaceID, installOne); err != nil {
		t.Fatalf("seed additional groups: %v", err)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testWorkspaceID)
	req := newRequestAs(testUserID, http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/dingtalk/groups", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	testHandler.ListDingTalkGroups(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list groups status = %d: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Groups                  []DingTalkGroupResponse `json:"groups"`
		GroupDiscoverySupported bool                    `json:"group_discovery_supported"`
		InactiveGroupCounts     map[string]int64        `json:"inactive_group_counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.GroupDiscoverySupported || len(response.Groups) != 1 || response.Groups[0].ConversationID != "cid-shared" ||
		response.Groups[0].ConversationTitle != "Platform" || len(response.Groups[0].Bots) != 2 {
		t.Fatalf("group response = %+v", response.Groups)
	}
	if response.Groups[0].Bots[0].BotName != "Release Bot" || response.Groups[0].Bots[1].BotName != "Support Bot" {
		t.Fatalf("group bots = %+v", response.Groups[0].Bots)
	}
	if response.Groups[0].Bots[0].MentionCount != 2 || response.Groups[0].Bots[0].LastActiveAt == "" {
		t.Fatalf("release bot activity = %+v", response.Groups[0].Bots[0])
	}
	if response.Groups[0].Bots[1].MentionCount != 1 || response.Groups[0].Bots[1].LastActiveAt == "" {
		t.Fatalf("support bot activity = %+v", response.Groups[0].Bots[1])
	}
	if response.InactiveGroupCounts[installOne] != 2 {
		t.Fatalf("inactive counts = %+v, want %s=2", response.InactiveGroupCounts, installOne)
	}

	inactiveReq := newRequestAs(testUserID, http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/dingtalk/groups?activity=inactive&installation_id="+installOne, nil)
	inactiveReq = inactiveReq.WithContext(context.WithValue(inactiveReq.Context(), chi.RouteCtxKey, rctx))
	inactiveRec := httptest.NewRecorder()
	testHandler.ListDingTalkGroups(inactiveRec, inactiveReq)
	if inactiveRec.Code != http.StatusOK {
		t.Fatalf("list inactive groups status = %d: %s", inactiveRec.Code, inactiveRec.Body.String())
	}
	if err := json.Unmarshal(inactiveRec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Groups) != 2 || response.Groups[0].ConversationID != "cid-z" ||
		response.Groups[1].ConversationID != "cid-untitled" {
		t.Fatalf("inactive groups = %+v", response.Groups)
	}

	agentRec := httptest.NewRecorder()
	testHandler.listDingTalkGroups(agentRec, req, parseUUID(testWorkspaceID), agentOne, nil)
	if agentRec.Code != http.StatusOK {
		t.Fatalf("list Agent groups status = %d: %s", agentRec.Code, agentRec.Body.String())
	}
	if err := json.Unmarshal(agentRec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Groups) != 1 || len(response.Groups[0].Bots) != 1 ||
		response.Groups[0].Bots[0].AgentID != agentOne {
		t.Fatalf("Agent-scoped group response leaked another bot: %+v", response.Groups)
	}
}

func TestListDingTalkGroupsDisabledDeploymentReturnsStableEmptyShape(t *testing.T) {
	rec := httptest.NewRecorder()
	(&Handler{}).ListDingTalkGroups(rec, httptest.NewRequest(http.MethodGet, "/api/workspaces/bad/dingtalk/groups", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "{\"groups\":[],\"group_discovery_supported\":true,\"inactive_group_counts\":{},\"bot_identities\":{}}\n" {
		t.Fatalf("disabled groups response = %d %q", rec.Code, rec.Body.String())
	}
}

func TestListDingTalkGroupsReturnsInternalErrorOnDatabaseFailure(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("handler database unavailable")
	}
	previousInstall := testHandler.DingTalkInstall
	testHandler.DingTalkInstall = &dingtalkintegration.InstallService{}
	t.Cleanup(func() { testHandler.DingTalkInstall = previousInstall })

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", testWorkspaceID)
	ctx, cancel := context.WithCancel(context.Background())
	ctx = middleware.SetMemberContext(ctx, testWorkspaceID, db.Member{Role: "owner"})
	cancel()
	req := newRequestAs(testUserID, http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/dingtalk/groups", nil)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	testHandler.ListDingTalkGroups(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("database failure status = %d, want 500: %s", rec.Code, rec.Body.String())
	}

	inactiveReq := newRequestAs(testUserID, http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/dingtalk/groups?activity=inactive&installation_id=d1473000-0000-4000-8000-000000000099", nil)
	inactiveReq = inactiveReq.WithContext(ctx)
	testutil.Call(t, func(w http.ResponseWriter, r *http.Request) {
		testHandler.listDingTalkGroups(w, r, parseUUID(testWorkspaceID), "", nil)
	}, inactiveReq).Want(http.StatusInternalServerError)
}
