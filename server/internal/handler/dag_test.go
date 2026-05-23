package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/dagcore"
)

func TestDAGGraphAnalysisEmptyWorkspace(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/dag/analysis", nil)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rec := httptest.NewRecorder()

	testHandler.DAGGraphAnalysis(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp["status"] != "computed" {
		t.Fatalf("expected status computed, got %v", resp["status"])
	}
	if resp["node_count"] == nil {
		t.Fatal("expected node_count")
	}
	if resp["edge_count"] == nil {
		t.Fatal("expected edge_count")
	}
	if resp["cycles"] == nil {
		t.Fatal("expected cycles")
	}
}

func TestDAGEventAppendValidation(t *testing.T) {
	// Missing agent_id should fail validation
	body, _ := json.Marshal(map[string]any{
		"event": dagcore.Event{
			ID:        "test-evt-1",
			RecordIDs: []string{"rec-1"},
			Operation: dagcore.OperationCreate,
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dag/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rec := httptest.NewRecorder()

	testHandler.DAGEventAppend(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid event, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDAGEventAppendValidEvent(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"event": dagcore.Event{
			ID:        "test-evt-2",
			RecordIDs: []string{"rec-2"},
			AgentID:   "test-agent",
			DVT: dagcore.DVT{
				Dot:     dagcore.Dot{AgentID: "test-agent", Counter: 1},
				Context: map[string]int64{"test-agent": 1},
			},
			Operation: dagcore.OperationCreate,
			Payload:   map[string]any{"type": "issue"},
			Reason:    "test",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dag/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rec := httptest.NewRecorder()

	testHandler.DAGEventAppend(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["event_id"] == nil {
		t.Fatal("expected event_id in response")
	}
	if resp["status"] != "appended" {
		t.Fatalf("expected status appended, got %v", resp["status"])
	}
}

func TestDAGGraphAnalysisAfterEvent(t *testing.T) {
	// First append an event
	body, _ := json.Marshal(map[string]any{
		"event": dagcore.Event{
			ID:        "test-evt-3",
			RecordIDs: []string{"rec-3"},
			AgentID:   "test-agent",
			DVT: dagcore.DVT{
				Dot:     dagcore.Dot{AgentID: "test-agent", Counter: 2},
				Context: map[string]int64{"test-agent": 2},
			},
			Operation: dagcore.OperationCreate,
			Payload:   map[string]any{"type": "issue"},
			Reason:    "test",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dag/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	rec := httptest.NewRecorder()
	testHandler.DAGEventAppend(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: expected 201, got %d", rec.Code)
	}

	// Now check analysis reflects the node
	req2 := httptest.NewRequest(http.MethodGet, "/api/dag/analysis", nil)
	req2.Header.Set("X-Workspace-ID", testWorkspaceID)
	rec2 := httptest.NewRecorder()
	testHandler.DAGGraphAnalysis(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should have at least 1 node now
	nodeCount, _ := resp["node_count"].(float64)
	if nodeCount < 1 {
		t.Fatalf("expected at least 1 node after event, got %v", nodeCount)
	}
}
