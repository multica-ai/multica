package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
