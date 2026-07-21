package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentRuntimeBinding_EndToEnd(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID, _, plainMemberID := runtimeVisibilityFixture(t)
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id,
			instructions, custom_env, custom_args
		)
		VALUES ($1, 'runtime-binding-test-agent', '', 'cloud', '{}'::jsonb,
			$2, 'workspace', 'public_to', 1, $3, '', '{}'::jsonb, '[]'::jsonb)
		RETURNING id
	`, testWorkspaceID, testRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create workspace-visible agent: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO agent_invocation_target (agent_id, target_type, target_id)
		VALUES ($1, 'workspace', $2)
	`, agentID, testWorkspaceID); err != nil {
		t.Fatalf("grant workspace agent access: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	body := map[string]any{"runtime_id": runtimeID}
	w := httptest.NewRecorder()
	req := withURLParam(newRequestAs(plainMemberID, http.MethodPut, "/api/agents/"+agentID+"/runtime-binding", body), "id", agentID)
	testHandler.UpsertMyAgentRuntimeBinding(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("private runtime bind as plain member: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("make runtime public: %v", err)
	}

	w = httptest.NewRecorder()
	req = withURLParam(newRequestAs(plainMemberID, http.MethodPut, "/api/agents/"+agentID+"/runtime-binding", body), "id", agentID)
	testHandler.UpsertMyAgentRuntimeBinding(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("public runtime bind as plain member: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentRuntimeBindingResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode upsert response: %v", err)
	}
	if !resp.Bound || resp.RuntimeID == nil || *resp.RuntimeID != runtimeID || resp.EffectiveRuntimeID != runtimeID {
		t.Fatalf("upsert response = %+v, want bound runtime %s", resp, runtimeID)
	}

	w = httptest.NewRecorder()
	req = withURLParam(newRequestAs(plainMemberID, http.MethodGet, "/api/agents/"+agentID+"/runtime-binding", nil), "id", agentID)
	testHandler.GetMyAgentRuntimeBinding(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get runtime binding: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp = AgentRuntimeBindingResponse{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if !resp.Bound || resp.EffectiveRuntimeID != runtimeID {
		t.Fatalf("get response = %+v, want bound runtime %s", resp, runtimeID)
	}

	w = httptest.NewRecorder()
	req = withURLParam(newRequestAs(plainMemberID, http.MethodDelete, "/api/agents/"+agentID+"/runtime-binding", nil), "id", agentID)
	testHandler.DeleteMyAgentRuntimeBinding(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete runtime binding: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	resp = AgentRuntimeBindingResponse{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if resp.Bound || resp.RuntimeID != nil || resp.EffectiveRuntimeID != testRuntimeID {
		t.Fatalf("delete response = %+v, want unbound default runtime %s", resp, testRuntimeID)
	}
}
