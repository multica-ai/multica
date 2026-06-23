package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func createTestWorkflow(t *testing.T) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workflows", map[string]any{"title": "Stage Test Workflow"})
	testHandler.CreateWorkflow(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflow: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp.ID
}

func activateTestWorkflow(t *testing.T, workflowID string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PATCH", fmt.Sprintf("/api/workflows/%s", workflowID), map[string]any{"status": "active"})
	req = withURLParams(req, "id", workflowID)
	testHandler.UpdateWorkflow(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateWorkflow active: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func createTestWorkflowStage(t *testing.T, workflowID string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", fmt.Sprintf("/api/workflows/%s/stages", workflowID), map[string]any{"name": "Test Stage"})
	req = withURLParams(req, "id", workflowID)
	testHandler.CreateWorkflowStage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflowStage: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &resp)
	return resp.ID
}

func createTestWorkflowNodeWithStage(t *testing.T, workflowID, stageID string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", fmt.Sprintf("/api/workflows/%s/nodes", workflowID), map[string]any{
		"title":       "Test Node",
		"worker_type": "human",
		"critic_type": "human",
	})
	req = withURLParams(req, "id", workflowID)
	testHandler.CreateWorkflowNode(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflowNode: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var nr struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &nr)

	w = httptest.NewRecorder()
	req = newRequest("PUT", fmt.Sprintf("/api/workflows/%s/nodes/%s/stage", workflowID, nr.ID), map[string]any{"stage_id": stageID})
	req = withURLParams(req, "id", workflowID, "nodeId", nr.ID)
	testHandler.AssignNodeToStage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("AssignNodeToStage: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	return nr.ID
}

func TestCreateIssue_WithStageID(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	wfID := createTestWorkflow(t)
	stageID := createTestWorkflowStage(t, wfID)
	activateTestWorkflow(t, wfID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, wfID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Stage issue",
		"assignee_type": "workflow",
		"assignee_id":   wfID,
		"stage_id":      stageID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp IssueResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.StageID == nil || *resp.StageID != stageID {
		t.Fatalf("expected stage_id=%s, got %v", stageID, resp.StageID)
	}
}

func TestCreateIssue_StageIDRequiresWorkflow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	wfID := createTestWorkflow(t)
	stageID := createTestWorkflowStage(t, wfID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, wfID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "Stage without workflow",
		"stage_id": stageID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateIssue_StageIDMustBelongToWorkflow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	wf1 := createTestWorkflow(t)
	wf2 := createTestWorkflow(t)
	stage2 := createTestWorkflowStage(t, wf2)
	activateTestWorkflow(t, wf1)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, wf1)
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, wf2)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Wrong stage",
		"assignee_type": "workflow",
		"assignee_id":   wf1,
		"stage_id":      stage2,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateIssue_WorkflowChangeClearsStage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	wfID := createTestWorkflow(t)
	stageID := createTestWorkflowStage(t, wfID)
	activateTestWorkflow(t, wfID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, wfID)
	})

	issueID := createTestIssue(t, "Parent", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	// Assign issue to workflow with stage.
	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/issues/"+issueID, map[string]any{
		"assignee_type": "workflow",
		"assignee_id":   wfID,
		"stage_id":      stageID,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("assign workflow: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp IssueResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.StageID == nil || *resp.StageID != stageID {
		t.Fatalf("expected stage_id=%s after assignment, got %v", stageID, resp.StageID)
	}

	// Re-assign to member.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/issues/"+issueID, map[string]any{
		"assignee_type": "member",
		"assignee_id":   testUserID,
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("re-assign: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.StageID != nil {
		t.Fatalf("expected stage_id cleared, got %v", *resp.StageID)
	}
}

func TestCreateIssue_SubIssueInheritsStage(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	wfID := createTestWorkflow(t)
	stageID := createTestWorkflowStage(t, wfID)
	createTestWorkflowNodeWithStage(t, wfID, stageID)
	activateTestWorkflow(t, wfID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, wfID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Parent",
		"assignee_type": "workflow",
		"assignee_id":   wfID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp IssueResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	parentID := resp.ID
	t.Cleanup(func() { deleteTestIssue(t, parentID) })

	// Find sub-issue.
	children, err := testHandler.Queries.ListChildIssues(ctx, parseUUID(parentID))
	if err != nil {
		t.Fatalf("ListChildIssues: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("expected 1 sub-issue, got %d", len(children))
	}
	if !children[0].StageID.Valid {
		t.Fatalf("expected sub-issue stage_id set")
	}
	if children[0].StageID != parseUUID(stageID) {
		t.Fatalf("expected sub-issue stage_id=%s, got %v", stageID, children[0].StageID)
	}
}
