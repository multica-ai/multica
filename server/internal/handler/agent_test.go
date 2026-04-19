package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateAgent_RejectsDuplicateName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Clean up any agents created by this test.
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id = $1 AND name = $2`,
			testWorkspaceID, "duplicate-name-test-agent",
		)
	})

	body := map[string]any{
		"name":                 "duplicate-name-test-agent",
		"description":          "first description",
		"runtime_ids":          []string{testRuntimeID},
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

	// Second call — same name must be rejected with 409 Conflict.
	// The unique constraint prevents silent duplicates; the UI shows a clear error.
	body["description"] = "updated description"
	w2 := httptest.NewRecorder()
	testHandler.CreateAgent(w2, newRequest(http.MethodPost, "/api/agents", body))
	if w2.Code != http.StatusConflict {
		t.Fatalf("second CreateAgent with duplicate name: expected 409, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestCreateAgent_RejectsEmptyRuntimeIDs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	body := map[string]any{
		"name":                 "empty-rt-test",
		"runtime_ids":          []string{},
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty runtime_ids, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_RejectsRuntimeFromOtherWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	foreignRuntimeID := createRuntimeInOtherWorkspace(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, foreignRuntimeID)
	})
	body := map[string]any{
		"name":                 "foreign-rt-test",
		"runtime_ids":          []string{foreignRuntimeID},
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for cross-workspace runtime, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateAgent_AssignsMultipleRuntimes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	rt2 := createSecondRuntimeInTestWorkspace(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, rt2)
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE workspace_id = $1 AND name = 'multi-rt-test'`, testWorkspaceID)
	})
	body := map[string]any{
		"name":                 "multi-rt-test",
		"runtime_ids":          []string{testRuntimeID, rt2},
		"visibility":           "private",
		"max_concurrent_tasks": 1,
	}
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequest(http.MethodPost, "/api/agents", body))
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	runtimes, _ := resp["runtimes"].([]any)
	if len(runtimes) != 2 {
		t.Fatalf("expected 2 runtimes in response, got %d", len(runtimes))
	}
}

func TestUpdateAgent_ReplacesRuntimeSet(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createAgentWithRuntimes(t, []string{testRuntimeID})
	rt2 := createSecondRuntimeInTestWorkspace(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, rt2)
	})
	body := map[string]any{"runtime_ids": []string{rt2}}
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, newAuthedRequestWithPath(http.MethodPatch, "/api/agents/"+agentID, body, agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var count int
	testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_runtime_assignment WHERE agent_id = $1`, agentID).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 assignment after replace, got %d", count)
	}
}

func TestUpdateAgent_RejectsEmptyRuntimeIDs(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createAgentWithRuntimes(t, []string{testRuntimeID})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })
	body := map[string]any{"runtime_ids": []string{}}
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, newAuthedRequestWithPath(http.MethodPatch, "/api/agents/"+agentID, body, agentID))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestEnqueueTaskForIssue_DistributesAcrossRuntimes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	rt2 := createSecondRuntimeInTestWorkspace(t)
	rt3 := createThirdRuntimeInTestWorkspace(t)
	agentID := createAgentWithRuntimes(t, []string{testRuntimeID, rt2, rt3})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id IN ($1, $2)`, rt2, rt3)
	})

	counts := map[string]int{}
	for i := 0; i < 9; i++ {
		issueID := createIssueAssignedTo(t, agentID)
		issue := loadIssue(t, issueID)
		task, err := testHandler.TaskService.EnqueueTaskForIssue(context.Background(), issue)
		if err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
		counts[uuidToString(task.RuntimeID)]++
	}

	for _, rt := range []string{testRuntimeID, rt2, rt3} {
		if counts[rt] < 2 || counts[rt] > 4 {
			t.Fatalf("expected 2-4 tasks on runtime %s, got %d (all counts: %+v)", rt, counts[rt], counts)
		}
	}
}

func TestUpdateAgent_PreservesCreatedAtOfSurvivingAssignments(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createAgentWithRuntimes(t, []string{testRuntimeID})
	rt2 := createSecondRuntimeInTestWorkspace(t)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, rt2)
	})
	var origCreatedAt time.Time
	testPool.QueryRow(context.Background(),
		`SELECT created_at FROM agent_runtime_assignment WHERE agent_id = $1 AND runtime_id = $2`,
		agentID, testRuntimeID,
	).Scan(&origCreatedAt)
	body := map[string]any{"runtime_ids": []string{testRuntimeID, rt2}}
	w := httptest.NewRecorder()
	testHandler.UpdateAgent(w, newAuthedRequestWithPath(http.MethodPatch, "/api/agents/"+agentID, body, agentID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var newCreatedAt time.Time
	testPool.QueryRow(context.Background(),
		`SELECT created_at FROM agent_runtime_assignment WHERE agent_id = $1 AND runtime_id = $2`,
		agentID, testRuntimeID,
	).Scan(&newCreatedAt)
	if !origCreatedAt.Equal(newCreatedAt) {
		t.Fatalf("surviving assignment created_at changed: %v → %v", origCreatedAt, newCreatedAt)
	}
}
