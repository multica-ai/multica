package handler

// CEREBRO-PATCH(workflow-open-step-tool-test): FIR-3493 preserves the
// task-scoped workflow capability while legacy agent-tool authoring stays
// retired by FIR-3403.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTaskAllowsOpenLoopStepOnlyForBoundedStepsContext(t *testing.T) {
	if !taskAllowsOpenLoopStep(json.RawMessage(`{"loop_step":{"steps":{"allowed":true,"max":2}}}`)) {
		t.Fatal("bounded steps context was not recognised")
	}
	for _, raw := range []string{
		`{"loop_step":{"steps":{"allowed":false,"max":2}}}`,
		`{"loop_step":{"steps":{"allowed":true,"max":0}}}`,
		`{}`,
		`not-json`,
	} {
		if taskAllowsOpenLoopStep(json.RawMessage(raw)) {
			t.Fatalf("unbounded or invalid context was accepted: %s", raw)
		}
	}
}

func TestListAgentToolsOffersOpenLoopStepOnlyForBoundedTask(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "agent-tools-open-loop-step", []byte(`{}`))
	taskID := createHandlerTestDelegatedTaskForAgent(t, agentID, testUserID)
	if _, err := testPool.Exec(ctx,
		`UPDATE agent_task_queue
		 SET context = '{"loop_step":{"steps":{"allowed":true,"max":2}}}'::jsonb
		 WHERE id = $1`,
		taskID,
	); err != nil {
		t.Fatalf("set task loop context: %v", err)
	}

	prevItems := testHandler.cerebroToolItems
	prevDesc := testHandler.cerebroToolDesc
	prevStatus := testHandler.cerebroToolStatus
	testHandler.SetCerebroToolMeta(nil)
	t.Cleanup(func() {
		testHandler.cerebroToolItems = prevItems
		testHandler.cerebroToolDesc = prevDesc
		testHandler.cerebroToolStatus = prevStatus
	})

	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodGet, "/api/agents/"+agentID+"/tools", nil), "id", agentID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	testHandler.ListAgentTools(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListAgentTools: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var tools []AgentToolResponse
	if err := json.NewDecoder(w.Body).Decode(&tools); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "open_loop_step" || !tools[0].Enabled || len(tools[0].InputSchema) == 0 {
		t.Fatalf("task-scoped open_loop_step tool mismatch: %+v", tools)
	}
}
