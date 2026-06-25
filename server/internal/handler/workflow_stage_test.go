package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCreateStage_InWorkflow creates a stage and verifies it appears in the
// workflow response.
func TestCreateStage_InWorkflow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Create a workflow to host the stage
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workflows", map[string]any{
		"title": "Stage Test Workflow",
	})
	testHandler.CreateWorkflow(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflow: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var createResp struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	wfID := createResp.ID
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, wfID)
	})

	// Create a stage
	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/stages", wfID), map[string]any{
		"name":        "需求",
		"description": "需求收集与分析",
	})
	req = withURLParams(req, "id", wfID)
	testHandler.CreateWorkflowStage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflowStage: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify stage appears in GetWorkflow
	w = httptest.NewRecorder()
	req = newRequest("GET", fmt.Sprintf("/api/workflows/%s", wfID), nil)
	req = withURLParams(req, "id", wfID)
	testHandler.GetWorkflow(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetWorkflow: expected 200, got %d", w.Code)
	}
	var getResp struct {
		Stages []struct {
			Name      string `json:"name"`
			NodeCount int64  `json:"node_count"`
		} `json:"stages"`
	}
	json.Unmarshal(w.Body.Bytes(), &getResp)
	if len(getResp.Stages) != 1 {
		t.Fatalf("expected 1 stage, got %d", len(getResp.Stages))
	}
	if getResp.Stages[0].Name != "需求" {
		t.Fatalf("stage name mismatch: got %q", getResp.Stages[0].Name)
	}
}

// TestCrossStageEdge_Allowed verifies that creating an edge between nodes
// in different stages succeeds (cross-stage edges are supported for panorama view).
func TestCrossStageEdge_Allowed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	// Create workflow
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workflows", map[string]any{"title": "Edge Validation WF"})
	testHandler.CreateWorkflow(w, req)
	var cr struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &cr)
	wfID := cr.ID
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, wfID)
	})

	// Create two stages
	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/stages", wfID), map[string]any{"name": "Stage A"})
	req = withURLParams(req, "id", wfID)
	testHandler.CreateWorkflowStage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflowStage A: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var sr1 struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &sr1)

	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/stages", wfID), map[string]any{"name": "Stage B"})
	req = withURLParams(req, "id", wfID)
	testHandler.CreateWorkflowStage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflowStage B: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var sr2 struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &sr2)

	// Create nodes in different stages
	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/nodes", wfID), map[string]any{
		"title":       "Node A",
		"worker_type": "agent",
		"critic_type": "human",
	})
	req = withURLParams(req, "id", wfID)
	testHandler.CreateWorkflowNode(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflowNode A: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var nr1 struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &nr1)

	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/nodes", wfID), map[string]any{
		"title":       "Node B",
		"worker_type": "agent",
		"critic_type": "human",
	})
	req = withURLParams(req, "id", wfID)
	testHandler.CreateWorkflowNode(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflowNode B: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var nr2 struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &nr2)

	// Assign nodes to different stages
	assignNode := func(nodeID, stageID string) {
		w := httptest.NewRecorder()
		req := newRequest("PUT",
			fmt.Sprintf("/api/workflows/%s/nodes/%s/stage", wfID, nodeID),
			map[string]any{"stage_id": stageID})
		req = withURLParams(req, "id", wfID, "nodeId", nodeID)
		testHandler.AssignNodeToStage(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("AssignNodeToStage: expected 200, got %d: %s", w.Code, w.Body.String())
		}
	}
	assignNode(nr1.ID, sr1.ID)
	assignNode(nr2.ID, sr2.ID)

	// Try cross-stage edge — should succeed
	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/edges", wfID), map[string]any{
		"source_node_id": nr1.ID,
		"target_node_id": nr2.ID,
	})
	req = withURLParams(req, "id", wfID)
	testHandler.CreateWorkflowEdge(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("cross-stage edge: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDeleteStage_SetsNodeStageNull verifies ON DELETE SET NULL behavior.
func TestDeleteStage_SetsNodeStageNull(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workflows", map[string]any{"title": "Delete Stage WF"})
	testHandler.CreateWorkflow(w, req)
	var cr struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &cr)
	wfID := cr.ID
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM multica_workflow WHERE id = $1`, wfID)
	})

	// Create stage + node assigned to it
	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/stages", wfID), map[string]any{"name": "S"})
	req = withURLParams(req, "id", wfID)
	testHandler.CreateWorkflowStage(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflowStage: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var sr struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &sr)

	w = httptest.NewRecorder()
	req = newRequest("POST", fmt.Sprintf("/api/workflows/%s/nodes", wfID), map[string]any{
		"title":       "N",
		"worker_type": "agent",
		"critic_type": "human",
	})
	req = withURLParams(req, "id", wfID)
	testHandler.CreateWorkflowNode(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkflowNode: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var nr struct{ ID string `json:"id"` }
	json.Unmarshal(w.Body.Bytes(), &nr)

	// Assign node to stage
	assignNode := func(nodeID, stageID string) {
		w := httptest.NewRecorder()
		req := newRequest("PUT",
			fmt.Sprintf("/api/workflows/%s/nodes/%s/stage", wfID, nodeID),
			map[string]any{"stage_id": stageID})
		req = withURLParams(req, "id", wfID, "nodeId", nodeID)
		testHandler.AssignNodeToStage(w, req)
	}
	assignNode(nr.ID, sr.ID)

	// Delete stage
	w = httptest.NewRecorder()
	req = newRequest("DELETE", fmt.Sprintf("/api/workflows/%s/stages/%s", wfID, sr.ID), nil)
	req = withURLParams(req, "id", wfID, "stageId", sr.ID)
	testHandler.DeleteWorkflowStage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteWorkflowStage: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify node's stage_id is NULL
	var stageID *string
	err := testPool.QueryRow(ctx,
		`SELECT stage_id::text FROM multica_workflow_node WHERE id = $1`, nr.ID,
	).Scan(&stageID)
	if err != nil {
		t.Fatalf("query node: %v", err)
	}
	if stageID != nil {
		t.Fatalf("expected NULL stage_id after stage delete, got %q", *stageID)
	}
}
