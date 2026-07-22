package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestAgentBuilderInstructionsConstrainModelsToRuntimeCatalog(t *testing.T) {
	for _, requirement := range []string{
		"AVAILABLE RUNTIME MODELS",
		"Never use a model label as the id",
		"never invent a model id",
	} {
		if !strings.Contains(agentBuilderInstructions, requirement) {
			t.Fatalf("agent builder instructions missing model constraint %q", requirement)
		}
	}
}

func TestCreateAgentBuilderSessionCreatesIsolatedHiddenBuilder(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM agent
			WHERE workspace_id = $1 AND kind = 'system' AND system_key LIKE 'agent_builder:%'
		`, testWorkspaceID)
	})

	create := func(model string) CreateAgentBuilderSessionResponse {
		w := httptest.NewRecorder()
		testHandler.CreateAgentBuilderSession(w, newRequest(http.MethodPost, "/api/agent-builder/sessions", map[string]any{
			"runtime_id": testRuntimeID,
			"model":      model,
		}))
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateAgentBuilderSession: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var response CreateAgentBuilderSessionResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if response.SessionID == "" || response.BuilderAgentID == "" {
			t.Fatalf("missing builder identifiers: %+v", response)
		}
		return response
	}

	first := create("builder-model-a")
	second := create("builder-model-b")
	if first.BuilderAgentID == second.BuilderAgentID {
		t.Fatalf("builder sessions unexpectedly shared an agent: %s", first.BuilderAgentID)
	}
	if first.SessionID == second.SessionID {
		t.Fatalf("each creation flow must receive a fresh chat session")
	}

	var kind, systemKey, firstModel string
	if err := testPool.QueryRow(context.Background(), `
		SELECT kind, system_key, model FROM agent WHERE id = $1
	`, first.BuilderAgentID).Scan(&kind, &systemKey, &firstModel); err != nil {
		t.Fatalf("load builder agent: %v", err)
	}
	if kind != "system" || !strings.HasPrefix(systemKey, "agent_builder:") {
		t.Fatalf("unexpected builder identity kind=%q system_key=%q", kind, systemKey)
	}
	if firstModel != "builder-model-a" {
		t.Fatalf("first builder model was mutated: got %q", firstModel)
	}

	w := httptest.NewRecorder()
	testHandler.ListAgents(w, newRequest(http.MethodGet, "/api/agents", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgents: %d: %s", w.Code, w.Body.String())
	}
	var listed []AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode agent list: %v", err)
	}
	for _, agent := range listed {
		if agent.ID == first.BuilderAgentID {
			t.Fatalf("system builder leaked into the user-facing agent list")
		}
	}

	// Knowing the ID must not expose system infrastructure through the public
	// Agent detail/update/archive loaders.
	w = httptest.NewRecorder()
	req := withURLParams(newRequest(http.MethodGet, "/api/agents/"+first.BuilderAgentID, nil), "id", first.BuilderAgentID)
	testHandler.GetAgent(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetAgent(system): expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Deleting the private Builder chat also removes its session-scoped hidden
	// Agent, so completed/cancelled flows do not accumulate infrastructure rows.
	w = httptest.NewRecorder()
	req = withURLParams(newRequest(http.MethodDelete, "/api/chat/sessions/"+first.SessionID, nil), "sessionId", first.SessionID)
	req = withChatTestWorkspaceCtx(t, req)
	testHandler.DeleteChatSession(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteChatSession(builder): expected 204, got %d: %s", w.Code, w.Body.String())
	}
	var remaining int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent WHERE id = $1`, first.BuilderAgentID).Scan(&remaining); err != nil {
		t.Fatalf("count deleted builder: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("builder agent survived chat deletion")
	}
}

func TestCreateAgentAttachesSkillsInCreateTransaction(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	var skillID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO skill (workspace_id, name, description, content, config, created_by)
		VALUES ($1, 'Atomic Create Skill', '', '# Atomic', '{}'::jsonb, $2)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&skillID); err != nil {
		t.Fatalf("create skill fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE workspace_id = $1 AND name = 'Atomic Skill Agent'`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM skill WHERE id = $1`, skillID)
	})

	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", map[string]any{
		"name":       "Atomic Skill Agent",
		"runtime_id": testRuntimeID,
		"skill_ids":  []string{skillID},
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgent: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var response AgentResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Skills) != 1 || response.Skills[0].ID != skillID {
		t.Fatalf("create response did not include attached skill: %+v", response.Skills)
	}
	var introSessions int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM chat_session WHERE agent_id = $1 AND is_agent_intro = true
	`, response.ID).Scan(&introSessions); err != nil {
		t.Fatalf("count welcome chat sessions: %v", err)
	}
	if introSessions != 1 {
		t.Fatalf("welcome chat sessions = %d, want 1", introSessions)
	}
}

// newBuilderSession starts a builder conversation on testRuntimeID and registers
// cleanup for the carrier agents the flow creates.
func newBuilderSession(t *testing.T) CreateAgentBuilderSessionResponse {
	t.Helper()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM agent
			WHERE workspace_id = $1 AND kind = 'system' AND system_key LIKE 'agent_builder:%'
		`, testWorkspaceID)
	})

	w := httptest.NewRecorder()
	testHandler.CreateAgentBuilderSession(w, newRequest(http.MethodPost, "/api/agent-builder/sessions", map[string]any{
		"runtime_id": testRuntimeID,
		"model":      "model-pinned-to-runtime-a",
	}))
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAgentBuilderSession: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var session CreateAgentBuilderSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return session
}

// newTestRuntime inserts an extra runtime in the fixture workspace so switch
// tests have somewhere to move to.
func newTestRuntime(t *testing.T, name, status string) string {
	t.Helper()
	var runtimeID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, $2, 'cloud', $3, $4, 'switch test runtime', '{}'::jsonb, $5, now())
		RETURNING id
	`, testWorkspaceID, name, strings.ToLower(strings.ReplaceAll(name, " ", "_")), status, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create runtime %q: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})
	return runtimeID
}

func switchBuilderRuntime(t *testing.T, sessionID, runtimeID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParams(
		newRequest(http.MethodPatch, "/api/agent-builder/sessions/"+sessionID+"/runtime", map[string]any{
			"runtime_id": runtimeID,
		}),
		"sessionId", sessionID,
	)
	testHandler.SwitchAgentBuilderRuntime(w, req)
	return w
}

func TestSwitchAgentBuilderRuntimeRebindsCarrier(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	session := newBuilderSession(t)
	target := newTestRuntime(t, "Builder Switch Target", "online")

	w := switchBuilderRuntime(t, session.SessionID, target)
	if w.Code != http.StatusOK {
		t.Fatalf("SwitchAgentBuilderRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var response SwitchAgentBuilderRuntimeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode switch response: %v", err)
	}
	if response.RuntimeID != target {
		t.Fatalf("response runtime = %q, want %q", response.RuntimeID, target)
	}

	// The carrier is what stamps a chat task's runtime, so this row — not the
	// client's local selection — is the fix.
	var boundRuntimeID, boundRuntimeMode string
	var boundModel pgtype.Text
	if err := testPool.QueryRow(ctx, `
		SELECT runtime_id::text, runtime_mode, model FROM agent WHERE id = $1
	`, session.BuilderAgentID).Scan(&boundRuntimeID, &boundRuntimeMode, &boundModel); err != nil {
		t.Fatalf("load builder carrier: %v", err)
	}
	if boundRuntimeID != target {
		t.Fatalf("carrier runtime = %q, want %q", boundRuntimeID, target)
	}
	if boundRuntimeMode != "cloud" {
		t.Fatalf("carrier runtime_mode = %q, want cloud", boundRuntimeMode)
	}
	if boundModel.Valid {
		t.Fatalf("carrier model should be cleared on rebind, got %q", boundModel.String)
	}

	// Left deliberately stale: the daemon only resumes a stored provider session
	// when this pointer matches the claiming task's runtime, so keeping the old
	// value is what makes the new runtime start a fresh session.
	var sessionRuntimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT runtime_id::text FROM chat_session WHERE id = $1
	`, session.SessionID).Scan(&sessionRuntimeID); err != nil {
		t.Fatalf("load chat session: %v", err)
	}
	if sessionRuntimeID != testRuntimeID {
		t.Fatalf("chat_session.runtime_id = %q, want the original %q", sessionRuntimeID, testRuntimeID)
	}
}

func TestSwitchAgentBuilderRuntimeRejectsOfflineTarget(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	session := newBuilderSession(t)
	offline := newTestRuntime(t, "Builder Switch Offline", "offline")

	if w := switchBuilderRuntime(t, session.SessionID, offline); w.Code != http.StatusConflict {
		t.Fatalf("offline target: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSwitchAgentBuilderRuntimeRejectsWhileReplyPending(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	session := newBuilderSession(t)
	target := newTestRuntime(t, "Builder Switch Pending Target", "online")

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, chat_session_id, status, priority, context, runtime_id)
		VALUES ($1, $2, 'running', 2, '{}'::jsonb, $3)
	`, session.BuilderAgentID, session.SessionID, testRuntimeID); err != nil {
		t.Fatalf("insert pending task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, session.SessionID)
	})

	if w := switchBuilderRuntime(t, session.SessionID, target); w.Code != http.StatusConflict {
		t.Fatalf("pending reply: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSwitchAgentBuilderRuntimeRejectsNonBuilderSession(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	target := newTestRuntime(t, "Builder Switch Foreign Target", "online")

	// A user-authored agent changes runtime through the agent update path; this
	// endpoint must not be a second way in.
	var userAgentID, userSessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id
		)
		VALUES ($1, 'Builder Switch User Agent', '', 'cloud', '{}'::jsonb, $2, 'workspace', 'public_to', 1, $3)
		RETURNING id
	`, testWorkspaceID, testRuntimeID, testUserID).Scan(&userAgentID); err != nil {
		t.Fatalf("create user agent: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id)
		VALUES ($1, $2, $3, 'Not a builder', $4)
		RETURNING id
	`, testWorkspaceID, userAgentID, testUserID, testRuntimeID).Scan(&userSessionID); err != nil {
		t.Fatalf("create user chat session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM chat_session WHERE id = $1`, userSessionID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, userAgentID)
	})

	if w := switchBuilderRuntime(t, userSessionID, target); w.Code != http.StatusNotFound {
		t.Fatalf("user agent session: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	var stillBound string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id::text FROM agent WHERE id = $1`, userAgentID).Scan(&stillBound); err != nil {
		t.Fatalf("reload user agent: %v", err)
	}
	if stillBound != testRuntimeID {
		t.Fatalf("user agent runtime changed to %q; the builder endpoint must never touch it", stillBound)
	}
}

// The regression this whole change exists for: a send that loaded the agent
// before a rebind committed must still enqueue on the runtime the session is
// bound to NOW. SendDirectChatMessage is handed the stale agent on purpose here
// — that is exactly what its caller does when the two requests interleave.
func TestSendDirectChatMessageUsesCurrentlyBoundRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	created := newBuilderSession(t)
	target := newTestRuntime(t, "Builder Send Rebind Target", "online")

	sessionUUID := parseUUID(created.SessionID)
	session, err := testHandler.Queries.GetChatSession(ctx, sessionUUID)
	if err != nil {
		t.Fatalf("load chat session: %v", err)
	}
	staleAgent, err := testHandler.Queries.GetAgent(ctx, parseUUID(created.BuilderAgentID))
	if err != nil {
		t.Fatalf("load builder carrier: %v", err)
	}
	if uuidToString(staleAgent.RuntimeID) != testRuntimeID {
		t.Fatalf("carrier should start on %q, got %q", testRuntimeID, uuidToString(staleAgent.RuntimeID))
	}

	if w := switchBuilderRuntime(t, created.SessionID, target); w.Code != http.StatusOK {
		t.Fatalf("SwitchAgentBuilderRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, created.SessionID)
	})

	sent, err := testHandler.TaskService.SendDirectChatMessage(
		ctx, session, staleAgent, parseUUID(testUserID), "hello after the switch", nil, "member", parseUUID(testUserID),
	)
	if err != nil {
		t.Fatalf("SendDirectChatMessage: %v", err)
	}
	if got := uuidToString(sent.Task.RuntimeID); got != target {
		t.Fatalf("task runtime = %q, want the rebound runtime %q — a stale in-flight send must not resurrect the old runtime", got, target)
	}
}
