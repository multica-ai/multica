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

func TestSwitchAgentBuilderRuntimeRebindsCarrier(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM agent
			WHERE workspace_id = $1 AND kind = 'system' AND system_key LIKE 'agent_builder:%'
		`, testWorkspaceID)
	})

	var otherRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, 'Builder Switch Runtime B', 'cloud', 'builder_switch_b', 'online', 'builder switch', '{}'::jsonb, $2, now())
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&otherRuntimeID); err != nil {
		t.Fatalf("create second runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, otherRuntimeID)
	})

	createW := httptest.NewRecorder()
	testHandler.CreateAgentBuilderSession(createW, newRequest(http.MethodPost, "/api/agent-builder/sessions", map[string]any{
		"runtime_id": testRuntimeID,
		"model":      "stale-model-a",
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateAgentBuilderSession: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var session CreateAgentBuilderSessionResponse
	if err := json.Unmarshal(createW.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	switchW := httptest.NewRecorder()
	req := withURLParams(
		newRequest(http.MethodPatch, "/api/agent-builder/sessions/"+session.SessionID+"/runtime", map[string]any{
			"runtime_id": otherRuntimeID,
		}),
		"sessionId", session.SessionID,
	)
	testHandler.SwitchAgentBuilderRuntime(switchW, req)
	if switchW.Code != http.StatusOK {
		t.Fatalf("SwitchAgentBuilderRuntime: expected 200, got %d: %s", switchW.Code, switchW.Body.String())
	}
	var switchResp SwitchAgentBuilderRuntimeResponse
	if err := json.Unmarshal(switchW.Body.Bytes(), &switchResp); err != nil {
		t.Fatalf("decode switch response: %v", err)
	}
	if switchResp.RuntimeID != otherRuntimeID {
		t.Fatalf("switch response runtime = %q, want %q", switchResp.RuntimeID, otherRuntimeID)
	}

	var agentRuntimeID, runtimeMode string
	var model pgtype.Text
	if err := testPool.QueryRow(ctx, `
		SELECT runtime_id::text, runtime_mode, model FROM agent WHERE id = $1
	`, session.BuilderAgentID).Scan(&agentRuntimeID, &runtimeMode, &model); err != nil {
		t.Fatalf("load builder agent: %v", err)
	}
	if agentRuntimeID != otherRuntimeID {
		t.Fatalf("builder agent runtime = %q, want %q", agentRuntimeID, otherRuntimeID)
	}
	if runtimeMode != "cloud" {
		t.Fatalf("builder agent runtime_mode = %q, want cloud", runtimeMode)
	}
	if model.Valid {
		t.Fatalf("builder agent model should be cleared, got %q", model.String)
	}

	var sessionRuntimeID string
	if err := testPool.QueryRow(ctx, `
		SELECT runtime_id::text FROM chat_session WHERE id = $1
	`, session.SessionID).Scan(&sessionRuntimeID); err != nil {
		t.Fatalf("load chat session runtime: %v", err)
	}
	if sessionRuntimeID != testRuntimeID {
		t.Fatalf("chat_session.runtime_id = %q, want original %q (resume pointer must stay)", sessionRuntimeID, testRuntimeID)
	}
}

func TestSwitchAgentBuilderRuntimeRejectsGuards(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM agent
			WHERE workspace_id = $1 AND kind = 'system' AND system_key LIKE 'agent_builder:%'
		`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `
			DELETE FROM agent WHERE workspace_id = $1 AND name = 'Builder Switch User Agent'
		`, testWorkspaceID)
	})

	var offlineRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, 'Builder Switch Offline', 'cloud', 'builder_switch_offline', 'offline', 'offline', '{}'::jsonb, $2, now())
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&offlineRuntimeID); err != nil {
		t.Fatalf("create offline runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, offlineRuntimeID)
	})

	createW := httptest.NewRecorder()
	testHandler.CreateAgentBuilderSession(createW, newRequest(http.MethodPost, "/api/agent-builder/sessions", map[string]any{
		"runtime_id": testRuntimeID,
	}))
	if createW.Code != http.StatusCreated {
		t.Fatalf("CreateAgentBuilderSession: expected 201, got %d: %s", createW.Code, createW.Body.String())
	}
	var session CreateAgentBuilderSessionResponse
	if err := json.Unmarshal(createW.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	offlineW := httptest.NewRecorder()
	offlineReq := withURLParams(
		newRequest(http.MethodPatch, "/api/agent-builder/sessions/"+session.SessionID+"/runtime", map[string]any{
			"runtime_id": offlineRuntimeID,
		}),
		"sessionId", session.SessionID,
	)
	testHandler.SwitchAgentBuilderRuntime(offlineW, offlineReq)
	if offlineW.Code != http.StatusConflict {
		t.Fatalf("offline runtime: expected 409, got %d: %s", offlineW.Code, offlineW.Body.String())
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, chat_session_id, status, priority, context, runtime_id)
		VALUES ($1, $2, 'running', 2, '{}'::jsonb, $3)
	`, session.BuilderAgentID, session.SessionID, testRuntimeID); err != nil {
		t.Fatalf("insert pending task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE chat_session_id = $1`, session.SessionID)
	})

	var onlineRuntimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at
		)
		VALUES ($1, NULL, 'Builder Switch Pending Target', 'cloud', 'builder_switch_pending', 'online', 'online', '{}'::jsonb, $2, now())
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&onlineRuntimeID); err != nil {
		t.Fatalf("create online runtime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, onlineRuntimeID)
	})

	pendingW := httptest.NewRecorder()
	pendingReq := withURLParams(
		newRequest(http.MethodPatch, "/api/agent-builder/sessions/"+session.SessionID+"/runtime", map[string]any{
			"runtime_id": onlineRuntimeID,
		}),
		"sessionId", session.SessionID,
	)
	testHandler.SwitchAgentBuilderRuntime(pendingW, pendingReq)
	if pendingW.Code != http.StatusConflict {
		t.Fatalf("pending task: expected 409, got %d: %s", pendingW.Code, pendingW.Body.String())
	}

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
	})

	userW := httptest.NewRecorder()
	userReq := withURLParams(
		newRequest(http.MethodPatch, "/api/agent-builder/sessions/"+userSessionID+"/runtime", map[string]any{
			"runtime_id": onlineRuntimeID,
		}),
		"sessionId", userSessionID,
	)
	testHandler.SwitchAgentBuilderRuntime(userW, userReq)
	if userW.Code != http.StatusNotFound {
		t.Fatalf("user agent session: expected 404, got %d: %s", userW.Code, userW.Body.String())
	}
}
