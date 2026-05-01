package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPauseWorkspaceTasksTogglesFlag verifies the kill switch toggles the
// settings.tasks_paused flag on the workspace and returns the updated workspace
// in the response.
func TestPauseWorkspaceTasksTogglesFlag(t *testing.T) {
	t.Cleanup(func() {
		// Always release the switch after the test so no later test sees a
		// paused workspace.
		_, _ = testPool.Exec(context.Background(),
			`UPDATE workspace SET settings = settings - 'tasks_paused' WHERE id = $1`,
			testWorkspaceID,
		)
	})

	// Engage
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/pause-tasks", map[string]any{
		"paused": true,
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.PauseWorkspaceTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PauseWorkspaceTasks engage: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var engaged map[string]any
	if err := json.NewDecoder(w.Body).Decode(&engaged); err != nil {
		t.Fatalf("decode engage response: %v", err)
	}
	if _, ok := engaged["workspace"]; !ok {
		t.Fatalf("engage response missing workspace key: %v", engaged)
	}
	if _, ok := engaged["cancelled_count"]; !ok {
		t.Fatalf("engage response missing cancelled_count key: %v", engaged)
	}

	if !testHandler.TaskService.IsWorkspacePaused(context.Background(), parseUUID(testWorkspaceID)) {
		t.Fatal("expected IsWorkspacePaused=true after engage")
	}

	// Release
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/pause-tasks", map[string]any{
		"paused": false,
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.PauseWorkspaceTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PauseWorkspaceTasks release: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if testHandler.TaskService.IsWorkspacePaused(context.Background(), parseUUID(testWorkspaceID)) {
		t.Fatal("expected IsWorkspacePaused=false after release")
	}
}

// TestPauseWorkspaceTasksBlocksNewIssueTasks verifies that creating a
// todo issue assigned to an agent while paused does NOT enqueue a task —
// the kill switch's primary purpose.
func TestPauseWorkspaceTasksBlocksNewIssueTasks(t *testing.T) {
	ctx := context.Background()

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx,
			`UPDATE workspace SET settings = settings - 'tasks_paused' WHERE id = $1`,
			testWorkspaceID,
		)
	})

	// Engage
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/pause-tasks", map[string]any{
		"paused": true,
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.PauseWorkspaceTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("engage: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Find the test agent
	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("find test agent: %v", err)
	}

	// Create a todo issue assigned to the agent — would normally enqueue a task.
	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Paused workspace issue",
		"status":        "todo",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
	})

	var taskCount int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`,
		created.ID,
	).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if taskCount != 0 {
		t.Fatalf("expected 0 tasks for issue created while workspace paused, got %d", taskCount)
	}
}

// TestPauseWorkspaceTasksCancelsActiveTasks verifies that engaging the
// kill switch cancels every queued/dispatched/running task in the workspace
// and returns an accurate cancelled_count.
func TestPauseWorkspaceTasksCancelsActiveTasks(t *testing.T) {
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("find test agent: %v", err)
	}

	// Two issues with this agent assigned, status=todo → two queued tasks.
	createIssueAndTask := func(title string) (issueID, taskID string) {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title":         title,
			"status":        "todo",
			"assignee_type": "agent",
			"assignee_id":   agentID,
		})
		testHandler.CreateIssue(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("CreateIssue: expected 201, got %d: %s", w.Code, w.Body.String())
		}
		var created IssueResponse
		json.NewDecoder(w.Body).Decode(&created)
		issueID = created.ID

		if err := testPool.QueryRow(ctx,
			`SELECT id FROM agent_task_queue WHERE issue_id = $1 LIMIT 1`,
			issueID,
		).Scan(&taskID); err != nil {
			t.Fatalf("expected one queued task for issue %s: %v", issueID, err)
		}
		return
	}

	issue1, _ := createIssueAndTask("Pause cancel test 1")
	issue2, _ := createIssueAndTask("Pause cancel test 2")
	t.Cleanup(func() {
		for _, id := range []string{issue1, issue2} {
			testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, id)
			cleanupReq := newRequest("DELETE", "/api/issues/"+id, nil)
			cleanupReq = withURLParam(cleanupReq, "id", id)
			testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
		}
		_, _ = testPool.Exec(ctx,
			`UPDATE workspace SET settings = settings - 'tasks_paused' WHERE id = $1`,
			testWorkspaceID,
		)
	})

	// Engage — should cancel both tasks.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/pause-tasks", map[string]any{
		"paused": true,
	})
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.PauseWorkspaceTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PauseWorkspaceTasks: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		CancelledCount int `json:"cancelled_count"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.CancelledCount != 2 {
		t.Fatalf("expected cancelled_count=2, got %d", resp.CancelledCount)
	}

	// Verify the tasks really are cancelled in DB.
	for _, id := range []string{issue1, issue2} {
		var status string
		if err := testPool.QueryRow(ctx,
			`SELECT status FROM agent_task_queue WHERE issue_id = $1`, id,
		).Scan(&status); err != nil {
			t.Fatalf("read task status for issue %s: %v", id, err)
		}
		if status != "cancelled" {
			t.Fatalf("issue %s task: expected status=cancelled, got %s", id, status)
		}
	}
}

// TestPauseWorkspaceTasksReleasePreservesCancelled verifies that releasing the
// kill switch does NOT resurrect previously-cancelled tasks — they stay
// cancelled, and the user is expected to re-trigger work explicitly.
func TestPauseWorkspaceTasksReleasePreservesCancelled(t *testing.T) {
	ctx := context.Background()

	var agentID string
	if err := testPool.QueryRow(ctx,
		`SELECT id FROM agent WHERE workspace_id = $1 AND name = $2`,
		testWorkspaceID, "Handler Test Agent",
	).Scan(&agentID); err != nil {
		t.Fatalf("find test agent: %v", err)
	}

	// Create a queued task
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":         "Release preserves cancellation",
		"status":        "todo",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: %d %s", w.Code, w.Body.String())
	}
	var created IssueResponse
	json.NewDecoder(w.Body).Decode(&created)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, created.ID)
		cleanupReq := newRequest("DELETE", "/api/issues/"+created.ID, nil)
		cleanupReq = withURLParam(cleanupReq, "id", created.ID)
		testHandler.DeleteIssue(httptest.NewRecorder(), cleanupReq)
		_, _ = testPool.Exec(ctx,
			`UPDATE workspace SET settings = settings - 'tasks_paused' WHERE id = $1`,
			testWorkspaceID,
		)
	})

	// Engage, then release.
	for _, paused := range []bool{true, false} {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/pause-tasks", map[string]any{
			"paused": paused,
		})
		req = withURLParam(req, "id", testWorkspaceID)
		testHandler.PauseWorkspaceTasks(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("toggle paused=%v: %d %s", paused, w.Code, w.Body.String())
		}
	}

	// Task must still be cancelled — releasing the switch does not resurrect it.
	var status string
	if err := testPool.QueryRow(ctx,
		`SELECT status FROM agent_task_queue WHERE issue_id = $1`, created.ID,
	).Scan(&status); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if status != "cancelled" {
		t.Fatalf("expected task to remain cancelled after release, got status=%s", status)
	}
}

// TestPauseWorkspaceTasksInvalidBodyReturnsBadRequest verifies that malformed
// JSON in the request body is rejected with a 400 instead of crashing.
func TestPauseWorkspaceTasksInvalidBodyReturnsBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(
		"POST",
		"/api/workspaces/"+testWorkspaceID+"/pause-tasks",
		nil,
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", testUserID)
	req.Body = http.NoBody
	req = withURLParam(req, "id", testWorkspaceID)
	testHandler.PauseWorkspaceTasks(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty body, got %d: %s", w.Code, w.Body.String())
	}
}
