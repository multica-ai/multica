package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// loadTestAgentID returns the seeded workspace-visible agent used by the
// handler test fixture.
func loadTestAgentID(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}
	return agentID
}

// TestCreateAutopilotWithMaxConcurrentRuns verifies the cap is persisted and
// echoed on the create response (WS-750).
func TestCreateAutopilotWithMaxConcurrentRuns(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := loadTestAgentID(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":               "cap-create",
		"assignee_id":         agentID,
		"execution_mode":      "create_issue",
		"max_concurrent_runs": 3,
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilot: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	defer testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, resp.ID)

	if resp.MaxConcurrentRuns == nil || *resp.MaxConcurrentRuns != 3 {
		t.Fatalf("response max_concurrent_runs = %+v, want 3", resp.MaxConcurrentRuns)
	}

	var dbCap *int32
	if err := testPool.QueryRow(ctx, `SELECT max_concurrent_runs FROM autopilot WHERE id = $1`, resp.ID).Scan(&dbCap); err != nil {
		t.Fatalf("read db cap: %v", err)
	}
	if dbCap == nil || *dbCap != 3 {
		t.Fatalf("db max_concurrent_runs = %+v, want 3", dbCap)
	}
}

// TestCreateAutopilotRejectsInvalidMaxConcurrentRuns locks the boundary
// validation: 0 and negatives are rejected at the API with a 400, not a 500
// from the DB CHECK constraint.
func TestCreateAutopilotRejectsInvalidMaxConcurrentRuns(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := loadTestAgentID(t)
	for _, cap := range []int{0, -1, -5} {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
			"title":               "cap-invalid",
			"assignee_id":         agentID,
			"execution_mode":      "create_issue",
			"max_concurrent_runs": cap,
		})
		testHandler.CreateAutopilot(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("cap=%d: expected 400, got %d: %s", cap, w.Code, w.Body.String())
		}
	}
}

// TestUpdateAutopilotMaxConcurrentRuns covers set / clear / omit semantics:
// setting a value writes it, sending null clears to unlimited, and omitting the
// field leaves the prior value untouched (WS-750).
func TestUpdateAutopilotMaxConcurrentRuns(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := loadTestAgentID(t)

	// Start with no cap.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "cap-update",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
	})
	testHandler.CreateAutopilot(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created AutopilotResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, created.ID)

	if created.MaxConcurrentRuns != nil {
		t.Fatalf("new autopilot should have no cap, got %d", *created.MaxConcurrentRuns)
	}

	// Set cap to 5.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+created.ID, map[string]any{
		"max_concurrent_runs": 5,
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update set: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated AutopilotResponse
	json.NewDecoder(w.Body).Decode(&updated)
	if updated.MaxConcurrentRuns == nil || *updated.MaxConcurrentRuns != 5 {
		t.Fatalf("after set: max_concurrent_runs = %+v, want 5", updated.MaxConcurrentRuns)
	}

	// Omit the field: prior value (5) must be preserved.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+created.ID, map[string]any{
		"title": "cap-update-renamed",
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update omit: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var omitted AutopilotResponse
	json.NewDecoder(w.Body).Decode(&omitted)
	if omitted.MaxConcurrentRuns == nil || *omitted.MaxConcurrentRuns != 5 {
		t.Fatalf("after omit: max_concurrent_runs = %+v, want 5 (unchanged)", omitted.MaxConcurrentRuns)
	}

	// Clear with null -> unlimited.
	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+created.ID, map[string]any{
		"max_concurrent_runs": nil,
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update clear: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var cleared AutopilotResponse
	json.NewDecoder(w.Body).Decode(&cleared)
	if cleared.MaxConcurrentRuns != nil {
		t.Fatalf("after clear: max_concurrent_runs = %d, want nil", *cleared.MaxConcurrentRuns)
	}
}

// TestUpdateAutopilotRejectsInvalidMaxConcurrentRuns mirrors the create-side
// boundary check on PATCH.
func TestUpdateAutopilotRejectsInvalidMaxConcurrentRuns(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := loadTestAgentID(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title":          "cap-update-invalid",
		"assignee_id":    agentID,
		"execution_mode": "create_issue",
	})
	testHandler.CreateAutopilot(w, req)
	var created AutopilotResponse
	json.NewDecoder(w.Body).Decode(&created)
	defer testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, created.ID)

	w = httptest.NewRecorder()
	req = newRequest("PATCH", "/api/autopilots/"+created.ID, map[string]any{
		"max_concurrent_runs": 0,
	})
	req = withURLParam(req, "id", created.ID)
	testHandler.UpdateAutopilot(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("update cap=0: expected 400, got %d: %s", w.Code, w.Body.String())
	}
}
