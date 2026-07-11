package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

func createAvailabilityTestAgent(
	t *testing.T,
	name, mode string,
	spaceIDs []string,
) AgentResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":                   name,
		"runtime_id":             handlerTestRuntimeID(t),
		"availability_mode":      mode,
		"availability_space_ids": spaceIDs,
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create availability agent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var agent AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&agent); err != nil {
		t.Fatalf("decode agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agent.ID)
	})
	return agent
}

func userListContainsAvailabilityAgent(t *testing.T, userID, agentID string) bool {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.ListAgents(w, newRequestAs(userID, http.MethodGet, "/api/agents?workspace_id="+testWorkspaceID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list agents: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var agents []AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
		t.Fatalf("decode agents: %v", err)
	}
	for _, agent := range agents {
		if agent.ID == agentID {
			return true
		}
	}
	return false
}

func TestAgentAvailabilitySelectedSpacesIsIndependentLocationGate(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	selected := createSpaceForAccessTest(t, "Agent Selected Space", "AGSEL", "private")
	other := createSpaceForAccessTest(t, "Agent Other Space", "AGOTH", "open")
	agent := createAvailabilityTestAgent(t, "selected-space-location-agent", agentAvailabilitySelectedSpaces, []string{selected.ID})

	if agent.AvailabilityMode != agentAvailabilitySelectedSpaces {
		t.Fatalf("availability_mode = %q, want selected_spaces", agent.AvailabilityMode)
	}
	if len(agent.AvailabilitySpaceIDs) != 1 || agent.AvailabilitySpaceIDs[0] != selected.ID {
		t.Fatalf("availability_space_ids = %v, want [%s]", agent.AvailabilitySpaceIDs, selected.ID)
	}
	if agent.PermissionMode != permissionModePublicTo {
		t.Fatalf("permission_mode = %q, want public_to", agent.PermissionMode)
	}

	loaded, err := testHandler.Queries.GetAgent(context.Background(), util.MustParseUUID(agent.ID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	selectedID := util.MustParseUUID(selected.ID)
	otherID := util.MustParseUUID(other.ID)
	// Even the owner cannot bypass the selected location.
	if !testHandler.canInvokeAgent(context.Background(), loaded, "member", testUserID, testUserID, testWorkspaceID, selectedID) {
		t.Fatal("owner should invoke in selected Space")
	}
	if testHandler.canInvokeAgent(context.Background(), loaded, "member", testUserID, testUserID, testWorkspaceID, otherID) {
		t.Fatal("owner must not invoke in an unselected Space")
	}
	if testHandler.canInvokeAgent(context.Background(), loaded, "member", testUserID, testUserID, testWorkspaceID, pgtype.UUID{}) {
		t.Fatal("Selected Spaces agent must fail closed without a Space context")
	}
}

func TestAgentAvailabilitySelectedSpaceDiscoveryFollowsSpaceVisibility(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	selected := createSpaceForAccessTest(t, "Private Agent Audience", "AGPRIV", "private")
	hidden := createSpaceForAccessTest(t, "Hidden Agent Audience", "AGHIDE", "private")
	agent := createAvailabilityTestAgent(t, "selected-space-discovery-agent", agentAvailabilitySelectedSpaces, []string{selected.ID, hidden.ID})
	allowedMember := createPermissionTestMember(t, "availability-allowed@multica.test")
	otherMember := createPermissionTestMember(t, "availability-other@multica.test")

	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO workspace_space_member (workspace_id, space_id, user_id, role, sort_order)
		VALUES ($1, $2, $3, 'member', 2)
	`, testWorkspaceID, selected.ID, allowedMember); err != nil {
		t.Fatalf("join selected Space: %v", err)
	}

	if !userListContainsAvailabilityAgent(t, allowedMember, agent.ID) {
		t.Fatal("member who can view a selected Private Space should discover the agent")
	}
	if userListContainsAvailabilityAgent(t, otherMember, agent.ID) {
		t.Fatal("member outside every selected Private Space must not discover the agent")
	}

	list := httptest.NewRecorder()
	testHandler.ListAgents(list, newRequestAs(allowedMember, http.MethodGet, "/api/agents?workspace_id="+testWorkspaceID, nil))
	var listed []AgentResponse
	if err := json.NewDecoder(list.Body).Decode(&listed); err != nil {
		t.Fatalf("decode listed agents: %v", err)
	}
	for _, got := range listed {
		if got.ID == agent.ID {
			if len(got.AvailabilitySpaceIDs) != 1 || got.AvailabilitySpaceIDs[0] != selected.ID {
				t.Fatalf("list leaked hidden selected Space IDs: %v", got.AvailabilitySpaceIDs)
			}
		}
	}

	detail := httptest.NewRecorder()
	testHandler.GetAgent(detail, withURLParam(newRequestAs(allowedMember, http.MethodGet, "/api/agents/"+agent.ID+"?workspace_id="+testWorkspaceID, nil), "id", agent.ID))
	if detail.Code != http.StatusOK {
		t.Fatalf("get agent: expected 200, got %d: %s", detail.Code, detail.Body.String())
	}
	var got AgentResponse
	if err := json.NewDecoder(detail.Body).Decode(&got); err != nil {
		t.Fatalf("decode agent detail: %v", err)
	}
	if len(got.AvailabilitySpaceIDs) != 1 || got.AvailabilitySpaceIDs[0] != selected.ID {
		t.Fatalf("detail leaked hidden selected Space IDs: %v", got.AvailabilitySpaceIDs)
	}
}

func TestAgentAvailabilityCreateOverridesLegacyAudienceFields(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID := createPermissionTestMember(t, "availability-create-legacy@multica.test")
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
		"name":            "availability-create-overrides-legacy",
		"runtime_id":      handlerTestRuntimeID(t),
		"permission_mode": permissionModePublicTo,
		"invocation_targets": []map[string]any{{
			"target_type": "member",
			"target_id":   memberID,
		}},
		"availability_mode":      agentAvailabilityWorkspace,
		"availability_space_ids": []string{},
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create agent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var agent AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&agent); err != nil {
		t.Fatalf("decode agent: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agent.ID)
	})

	var workspaceTargets, legacyTargets int
	if err := testPool.QueryRow(context.Background(), `
		SELECT
			count(*) FILTER (WHERE target_type = 'workspace'),
			count(*) FILTER (WHERE target_type IN ('member', 'team'))
		FROM agent_invocation_target
		WHERE agent_id = $1
	`, agent.ID).Scan(&workspaceTargets, &legacyTargets); err != nil {
		t.Fatalf("load invocation targets: %v", err)
	}
	if workspaceTargets != 1 || legacyTargets != 0 {
		t.Fatalf("targets workspace/legacy = %d/%d, want 1/0", workspaceTargets, legacyTargets)
	}
}

func TestAgentAvailabilityUpdateMigratesLegacyAudienceAtomically(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	selected := createSpaceForAccessTest(t, "Availability Update Space", "AGUPD", "open")
	memberID := createPermissionTestMember(t, "availability-legacy-target@multica.test")
	agentID := createPublicToAgentWithTargets(t, "availability-legacy-migration-agent", []map[string]any{
		{"target_type": "member", "target_id": memberID},
		{"target_type": "team", "target_id": "77777777-7777-7777-7777-777777777777"},
	})

	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"availability_mode":      agentAvailabilitySelectedSpaces,
		"availability_space_ids": []string{selected.ID},
	}), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("update selected availability: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var mode, permission string
	if err := testPool.QueryRow(context.Background(), `
		SELECT availability_mode, permission_mode FROM agent WHERE id = $1
	`, agentID).Scan(&mode, &permission); err != nil {
		t.Fatalf("load updated modes: %v", err)
	}
	if mode != agentAvailabilitySelectedSpaces || permission != permissionModePublicTo {
		t.Fatalf("modes = %s/%s, want selected_spaces/public_to", mode, permission)
	}
	var workspaceTargets, legacyTargets, availableSpaces int
	if err := testPool.QueryRow(context.Background(), `
		SELECT
			count(*) FILTER (WHERE target_type = 'workspace'),
			count(*) FILTER (WHERE target_type IN ('member', 'team'))
		FROM agent_invocation_target WHERE agent_id = $1
	`, agentID).Scan(&workspaceTargets, &legacyTargets); err != nil {
		t.Fatalf("load invocation targets: %v", err)
	}
	if workspaceTargets != 1 || legacyTargets != 0 {
		t.Fatalf("targets workspace/legacy = %d/%d, want 1/0 after explicit migration", workspaceTargets, legacyTargets)
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_available_space WHERE agent_id = $1 AND space_id = $2
	`, agentID, selected.ID).Scan(&availableSpaces); err != nil {
		t.Fatalf("load available Spaces: %v", err)
	}
	if availableSpaces != 1 {
		t.Fatalf("selected Space rows = %d, want 1", availableSpaces)
	}

	// Choosing Private is another explicit migration: it clears the shared
	// audience and selected-Space rows in the same transaction.
	w = httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID, map[string]any{
		"availability_mode":      agentAvailabilityPrivate,
		"availability_space_ids": []string{},
	}), "id", agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("update private availability: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var targetCount, spaceCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_invocation_target WHERE agent_id = $1`, agentID).Scan(&targetCount); err != nil {
		t.Fatalf("count private targets: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_available_space WHERE agent_id = $1`, agentID).Scan(&spaceCount); err != nil {
		t.Fatalf("count private Spaces: %v", err)
	}
	if targetCount != 0 || spaceCount != 0 {
		t.Fatalf("private rows targets/spaces = %d/%d, want 0/0", targetCount, spaceCount)
	}
}

func TestAgentProcessCannotChangeAvailability(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	selected := createSpaceForAccessTest(t, "Agent Actor Availability", "AGACT", "open")
	agent := createAvailabilityTestAgent(t, "agent-actor-availability", agentAvailabilityPrivate, nil)
	taskID := createHandlerTestTaskForAgent(t, agent.ID)

	req := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agent.ID, map[string]any{
		"availability_mode":      agentAvailabilitySelectedSpaces,
		"availability_space_ids": []string{selected.ID},
	}), "id", agent.ID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agent.ID)
	req.Header.Set("X-Task-ID", taskID)
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("agent-authored Availability update: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	req = withURLParam(newRequest(http.MethodPut, "/api/agents/"+agent.ID, map[string]any{
		"permission_mode": permissionModePublicTo,
		"invocation_targets": []map[string]any{{
			"target_type": invocationTargetWorkspace,
			"target_id":   testWorkspaceID,
		}},
	}), "id", agent.ID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agent.ID)
	req.Header.Set("X-Task-ID", taskID)
	w = httptest.NewRecorder()
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("agent-authored legacy access update: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

func TestLegacyAccessUpdateMapsLocationWithoutClobberingNoOpSelectedMode(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID := createPermissionTestMember(t, "availability-legacy-update@multica.test")
	privateAgent := createAvailabilityTestAgent(t, "availability-legacy-private-update", agentAvailabilityPrivate, nil)

	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(newRequest(http.MethodPut, "/api/agents/"+privateAgent.ID, map[string]any{
		"visibility": "workspace",
	}), "id", privateAgent.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("legacy workspace update: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var legacyUpdated AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&legacyUpdated); err != nil {
		t.Fatalf("decode legacy update: %v", err)
	}
	if legacyUpdated.AvailabilityMode != agentAvailabilityWorkspace {
		t.Fatalf("legacy update availability = %q, want workspace", legacyUpdated.AvailabilityMode)
	}
	loaded, err := testHandler.Queries.GetAgent(context.Background(), util.MustParseUUID(privateAgent.ID))
	if err != nil {
		t.Fatalf("load legacy-updated agent: %v", err)
	}
	if !testHandler.canInvokeAgent(context.Background(), loaded, "member", memberID, memberID, testWorkspaceID, pgtype.UUID{}) {
		t.Fatal("legacy workspace update should remain invocable by a workspace member")
	}

	selected := createSpaceForAccessTest(t, "No-op Legacy Selected", "AGNOP", "open")
	selectedAgent := createAvailabilityTestAgent(t, "availability-selected-noop-legacy", agentAvailabilitySelectedSpaces, []string{selected.ID})
	w = httptest.NewRecorder()
	testHandler.UpdateAgent(w, withURLParam(newRequest(http.MethodPut, "/api/agents/"+selectedAgent.ID, map[string]any{
		"name":            "availability-selected-noop-legacy-renamed",
		"permission_mode": permissionModePublicTo,
		"invocation_targets": []map[string]any{{
			"target_type": invocationTargetWorkspace,
			"target_id":   testWorkspaceID,
		}},
	}), "id", selectedAgent.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("no-op legacy echo: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var noOp AgentResponse
	if err := json.NewDecoder(w.Body).Decode(&noOp); err != nil {
		t.Fatalf("decode no-op update: %v", err)
	}
	if noOp.AvailabilityMode != agentAvailabilitySelectedSpaces || len(noOp.AvailabilitySpaceIDs) != 1 || noOp.AvailabilitySpaceIDs[0] != selected.ID {
		t.Fatalf("no-op legacy echo changed selected Availability: mode=%q spaces=%v", noOp.AvailabilityMode, noOp.AvailabilitySpaceIDs)
	}
}

func TestExistingAllSpacesChatNarrowsWithSelectedSpaceAvailability(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	selected := createSpaceForAccessTest(t, "Chat Selected Space", "AGCHAT", "open")
	agent := createAvailabilityTestAgent(t, "availability-existing-chat-agent", agentAvailabilityWorkspace, nil)
	var sessionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title)
		VALUES ($1, $2, $3, 'Existing availability chat')
		RETURNING id
	`, testWorkspaceID, agent.ID, testUserID).Scan(&sessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})

	updated := httptest.NewRecorder()
	testHandler.UpdateAgent(updated, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agent.ID, map[string]any{
		"availability_mode":      agentAvailabilitySelectedSpaces,
		"availability_space_ids": []string{selected.ID},
	}), "id", agent.ID))
	if updated.Code != http.StatusOK {
		t.Fatalf("change Availability: expected 200, got %d: %s", updated.Code, updated.Body.String())
	}

	sent := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/chat/sessions/"+sessionID+"/messages", map[string]any{
		"content": "try the stale session",
	}), "sessionId", sessionID)
	testHandler.SendChatMessage(sent, withChatTestWorkspaceCtx(t, req))
	if sent.Code != http.StatusCreated {
		t.Fatalf("send through All-spaces Chat: expected 201, got %d: %s", sent.Code, sent.Body.String())
	}

	session, err := testHandler.Queries.GetChatSession(context.Background(), parseUUID(sessionID))
	if err != nil {
		t.Fatalf("load chat session: %v", err)
	}
	loadedAgent, err := testHandler.Queries.GetAgent(context.Background(), parseUUID(agent.ID))
	if err != nil {
		t.Fatalf("load agent: %v", err)
	}
	scope, err := testHandler.TaskService.ChatSessionSpaceScope(context.Background(), session, loadedAgent, parseUUID(testUserID))
	if err != nil {
		t.Fatalf("resolve All-spaces scope: %v", err)
	}
	if !scope.All || len(scope.IDs) != 1 || uuidToString(scope.IDs[0]) != selected.ID {
		t.Fatalf("All-spaces scope = all:%v ids:%v, want selected Space %s", scope.All, scope.IDs, selected.ID)
	}
}

func TestExistingSingleSpaceChatCannotBypassSelectedSpaceAvailability(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	selected := createSpaceForAccessTest(t, "Chat Allowed Space", "AGCHATA", "open")
	other := createSpaceForAccessTest(t, "Chat Revoked Space", "AGCHATR", "open")
	agent := createAvailabilityTestAgent(t, "availability-single-chat-agent", agentAvailabilityWorkspace, nil)
	var sessionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, space_id)
		VALUES ($1, $2, $3, 'Existing single-Space chat', $4)
		RETURNING id
	`, testWorkspaceID, agent.ID, testUserID, other.ID).Scan(&sessionID); err != nil {
		t.Fatalf("seed chat session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, sessionID)
	})

	updated := httptest.NewRecorder()
	testHandler.UpdateAgent(updated, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agent.ID, map[string]any{
		"availability_mode":      agentAvailabilitySelectedSpaces,
		"availability_space_ids": []string{selected.ID},
	}), "id", agent.ID))
	if updated.Code != http.StatusOK {
		t.Fatalf("change Availability: expected 200, got %d: %s", updated.Code, updated.Body.String())
	}

	sent := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/chat/sessions/"+sessionID+"/messages", map[string]any{
		"content": "try the revoked Space",
	}), "sessionId", sessionID)
	testHandler.SendChatMessage(sent, withChatTestWorkspaceCtx(t, req))
	if sent.Code != http.StatusForbidden {
		t.Fatalf("send through revoked single-Space Chat: expected 403, got %d: %s", sent.Code, sent.Body.String())
	}
}

func TestCreateChatSessionSupportsAllAndSingleSpaceContext(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	selected := createSpaceForAccessTest(t, "Chat Context Selected", "AGCTXS", "open")
	other := createSpaceForAccessTest(t, "Chat Context Other", "AGCTXO", "open")
	agent := createAvailabilityTestAgent(t, "chat-context-create-agent", agentAvailabilitySelectedSpaces, []string{selected.ID})

	create := func(t *testing.T, spaceID *string, wantStatus int) *ChatSessionResponse {
		t.Helper()
		body := map[string]any{"agent_id": agent.ID, "title": "Context picker Chat"}
		if spaceID != nil {
			body["space_id"] = *spaceID
		}
		w := httptest.NewRecorder()
		req := withChatTestWorkspaceCtx(t, newRequest(http.MethodPost, "/api/chat/sessions", body))
		testHandler.CreateChatSession(w, req)
		if w.Code != wantStatus {
			t.Fatalf("create Chat with space %v: got %d, want %d: %s", spaceID, w.Code, wantStatus, w.Body.String())
		}
		if wantStatus != http.StatusCreated {
			return nil
		}
		var session ChatSessionResponse
		if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
			t.Fatalf("decode Chat session: %v", err)
		}
		return &session
	}

	all := create(t, nil, http.StatusCreated)
	if all.SpaceID != nil {
		t.Fatalf("All-spaces Chat returned space_id %v, want null", all.SpaceID)
	}
	single := create(t, &selected.ID, http.StatusCreated)
	if single.SpaceID == nil || *single.SpaceID != selected.ID {
		t.Fatalf("single-Space Chat returned space_id %v, want %s", single.SpaceID, selected.ID)
	}
	create(t, &other.ID, http.StatusForbidden)
}

func TestAgentAvailabilityRejectsForeignOrArchivedSpace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	archived := createSpaceForAccessTest(t, "Archived Agent Space", "AGARCH", "open")
	if _, err := testPool.Exec(context.Background(), `UPDATE workspace_space SET archived_at = now() WHERE id = $1`, archived.ID); err != nil {
		t.Fatalf("archive Space fixture: %v", err)
	}

	for _, spaceID := range []string{archived.ID, "99999999-9999-9999-9999-999999999999"} {
		w := httptest.NewRecorder()
		testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents?workspace_id="+testWorkspaceID, map[string]any{
			"name":                   "invalid-availability-" + spaceID[:8],
			"runtime_id":             handlerTestRuntimeID(t),
			"availability_mode":      agentAvailabilitySelectedSpaces,
			"availability_space_ids": []string{spaceID},
		}))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid Space %s: expected 400, got %d: %s", spaceID, w.Code, w.Body.String())
		}
	}
}

func TestIssueAndAutopilotWritesRejectAgentOutsideSelectedSpace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	selected := createSpaceForAccessTest(t, "Availability Work Space", "AGWORK", "open")
	other := createSpaceForAccessTest(t, "Availability Denied Space", "AGDENY", "open")
	agent := createAvailabilityTestAgent(t, "selected-space-write-gate-agent", agentAvailabilitySelectedSpaces, []string{selected.ID})

	createIssue := func(title, spaceID string, withAssignee bool) *httptest.ResponseRecorder {
		body := map[string]any{"title": title, "space_id": spaceID}
		if withAssignee {
			body["assignee_type"] = "agent"
			body["assignee_id"] = agent.ID
		}
		w := httptest.NewRecorder()
		testHandler.CreateIssue(w, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, body))
		return w
	}

	if w := createIssue("availability selected issue", selected.ID, true); w.Code != http.StatusCreated {
		t.Fatalf("selected-Space issue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if w := createIssue("availability denied issue", other.ID, true); w.Code != http.StatusForbidden {
		t.Fatalf("unselected-Space issue: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Updating an existing Issue must use that Issue's immutable Space, not a
	// missing request field or a workspace-wide fallback.
	unassigned := createIssue("availability update issue", other.ID, false)
	if unassigned.Code != http.StatusCreated {
		t.Fatalf("seed unassigned issue: %d: %s", unassigned.Code, unassigned.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(unassigned.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issue.ID, map[string]any{
		"assignee_type": "agent",
		"assignee_id":   agent.ID,
	}), "id", issue.ID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("assign existing unselected-Space issue: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	createAutopilot := func(title, spaceID string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.CreateAutopilot(w, newRequest(http.MethodPost, "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
			"title":          title,
			"space_id":       spaceID,
			"assignee_type":  "agent",
			"assignee_id":    agent.ID,
			"execution_mode": "run_only",
		}))
		return w
	}
	if w := createAutopilot("availability selected autopilot", selected.ID); w.Code != http.StatusCreated {
		t.Fatalf("selected-Space Autopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if w := createAutopilot("availability denied autopilot", other.ID); w.Code != http.StatusForbidden {
		t.Fatalf("unselected-Space Autopilot: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot WHERE workspace_id = $1 AND title LIKE 'availability % autopilot'`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE workspace_id = $1 AND title LIKE 'availability % issue'`, testWorkspaceID)
	})
}

func TestManualRerunRechecksSelectedSpaceAvailability(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	selected := createSpaceForAccessTest(t, "Rerun Selected Space", "AGRUN", "open")
	other := createSpaceForAccessTest(t, "Rerun New Selection", "AGNEW", "open")
	agent := createAvailabilityTestAgent(t, "selected-space-rerun-agent", agentAvailabilitySelectedSpaces, []string{selected.ID})

	created := httptest.NewRecorder()
	testHandler.CreateIssue(created, newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "availability rerun issue",
		"space_id":      selected.ID,
		"assignee_type": "agent",
		"assignee_id":   agent.ID,
	}))
	if created.Code != http.StatusCreated {
		t.Fatalf("seed issue: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(created.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	var sourceTaskID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT id FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
		ORDER BY created_at DESC LIMIT 1
	`, issue.ID, agent.ID).Scan(&sourceTaskID); err != nil {
		t.Fatalf("load source task: %v", err)
	}

	updated := httptest.NewRecorder()
	testHandler.UpdateAgent(updated, withURLParam(newRequest(http.MethodPut, "/api/agents/"+agent.ID, map[string]any{
		"availability_mode":      agentAvailabilitySelectedSpaces,
		"availability_space_ids": []string{other.ID},
	}), "id", agent.ID))
	if updated.Code != http.StatusOK {
		t.Fatalf("move Availability: expected 200, got %d: %s", updated.Code, updated.Body.String())
	}

	rerun := httptest.NewRecorder()
	testHandler.RerunIssue(rerun, withURLParam(newRequest(http.MethodPost, "/api/issues/"+issue.ID+"/rerun", map[string]any{
		"task_id": sourceTaskID,
	}), "id", issue.ID))
	if rerun.Code != http.StatusForbidden {
		t.Fatalf("rerun outside current Availability: expected 403, got %d: %s", rerun.Code, rerun.Body.String())
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
}

func TestBacklogPromotionRechecksPrivateAgentInvocation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	agentID, ownerID, memberID := privateAgentTestFixture(t)
	created := httptest.NewRecorder()
	testHandler.CreateIssue(created, newRequestAs(ownerID, http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "private availability backlog promotion",
		"status":        "backlog",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	}))
	if created.Code != http.StatusCreated {
		t.Fatalf("seed backlog issue: expected 201, got %d: %s", created.Code, created.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(created.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}

	updated := httptest.NewRecorder()
	testHandler.UpdateIssue(updated, withURLParam(newRequestAs(memberID, http.MethodPut, "/api/issues/"+issue.ID, map[string]any{
		"status": "todo",
	}), "id", issue.ID))
	if updated.Code != http.StatusOK {
		t.Fatalf("promote backlog issue: expected 200, got %d: %s", updated.Code, updated.Body.String())
	}
	var taskCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2
	`, issue.ID, agentID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("unauthorized backlog promotion enqueued %d private-agent tasks, want 0", taskCount)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issue.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issue.ID)
	})
}

func TestAutopilotSpaceCannotChange(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	restricted := createSpaceForAccessTest(t, "Restricted Autopilot Destination", "APREST", "private")
	memberID := createPlainMember(t, "autopilot-restricted-space@multica.test")
	autopilotID := createAutopilotAs(t, "", "availability-autopilot-space-move")
	grantAutopilotAccess(t, "", autopilotID, memberID, http.StatusCreated)

	w := httptest.NewRecorder()
	req := newRequestAs(memberID, http.MethodPatch, "/api/autopilots/"+autopilotID+"?workspace_id="+testWorkspaceID, map[string]any{
		"space_id": restricted.ID,
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("move Autopilot into another Space: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Autopilot Space cannot be changed") {
		t.Fatalf("move Autopilot into another Space: unexpected response: %s", w.Body.String())
	}
}
