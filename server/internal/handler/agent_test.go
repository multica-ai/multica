package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)


func TestCreateAgent_Idempotent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Guard: skip if the local DB is missing required columns (stale schema).
	var hasCustomArgs bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent' AND column_name='custom_args')`,
	).Scan(&hasCustomArgs); err != nil || !hasCustomArgs {
		t.Skip("agent table missing custom_args column — run migrations first")
	}

	// Clean up any agents created by this test.
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, "idempotent-test-agent",
		)
	})

	body := map[string]any{
		"name":                 "idempotent-test-agent",
		"description":          "first description",
		"runtime_id":           testRuntimeID,
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}

	// First call — creates the agent.
	w1 := httptest.NewRecorder()
	testHandler.CreateAgent(w1, newRequest(http.MethodPost, "/api/agents", body))
	if w1.Code != http.StatusCreated {
		t.Fatalf("first CreateAgent: expected 201, got %d: %s", w1.Code, w1.Body.String())
	}
	var resp1 map[string]any
	if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	agentID1, _ := resp1["id"].(string)
	if agentID1 == "" {
		t.Fatalf("first CreateAgent: no id in response: %v", resp1)
	}

	// Second call — same name, updated description.
	body["description"] = "updated description"
	w2 := httptest.NewRecorder()
	testHandler.CreateAgent(w2, newRequest(http.MethodPost, "/api/agents", body))
	if w2.Code != http.StatusCreated {
		t.Fatalf("second CreateAgent: expected 201, got %d: %s", w2.Code, w2.Body.String())
	}
	var resp2 map[string]any
	if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	agentID2, _ := resp2["id"].(string)

	// Must return the same agent, not a new one.
	if agentID1 != agentID2 {
		t.Fatalf("second CreateAgent created a duplicate: first=%s second=%s", agentID1, agentID2)
	}

	// Description should reflect the update.
	if desc, _ := resp2["description"].(string); desc != "updated description" {
		t.Fatalf("expected description 'updated description', got %q", desc)
	}
}
