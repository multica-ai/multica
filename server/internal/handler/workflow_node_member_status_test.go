package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateWorkflowNodeRejectsInactiveHumanWorker(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	pendingUserID := helperTestUser(t, "Pending Workflow Worker", "pending-workflow-worker@multica.ai")
	helperAddUserToWorkspaceWithStatus(t, pendingUserID, "member", "pending_activation")

	workflowID := createTestWorkflow(t)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, workflowID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", fmt.Sprintf("/api/workflows/%s/nodes", workflowID), map[string]any{
		"title":       "Inactive worker node",
		"worker_type": "human",
		"worker_id":   pendingUserID,
		"critic_type": "human",
	})
	req = withURLParams(req, "id", workflowID)
	testHandler.CreateWorkflowNode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("CreateWorkflowNode with inactive worker: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "inactive workspace member") {
		t.Fatalf("CreateWorkflowNode with inactive worker: expected inactive member error, got %s", w.Body.String())
	}
}

func TestUpdateWorkflowNodeRejectsInactiveHumanCritic(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	pendingUserID := helperTestUser(t, "Pending Workflow Critic", "pending-workflow-critic@multica.ai")
	helperAddUserToWorkspaceWithStatus(t, pendingUserID, "member", "pending_activation")

	workflowID := createTestWorkflow(t)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, workflowID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", fmt.Sprintf("/api/workflows/%s/nodes", workflowID), map[string]any{
		"title":       "Node to update",
		"worker_type": "human",
		"critic_type": "human",
	})
	req = withURLParams(req, "id", workflowID)
	testHandler.CreateWorkflowNode(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflowNode fixture: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode CreateWorkflowNode response: %v", err)
	}
	nodeID := createResp.ID

	w = httptest.NewRecorder()
	req = newRequest("PUT", fmt.Sprintf("/api/workflows/%s/nodes/%s", workflowID, nodeID), map[string]any{
		"critic_type": "human",
		"critic_id":   pendingUserID,
	})
	req = withURLParams(req, "id", workflowID, "nodeId", nodeID)
	testHandler.UpdateWorkflowNode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("UpdateWorkflowNode with inactive critic: expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "inactive workspace member") {
		t.Fatalf("UpdateWorkflowNode with inactive critic: expected inactive member error, got %s", w.Body.String())
	}
}
