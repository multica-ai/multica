package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListTasksByIssueMarksRecordedChanges(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "RecordedChangesAgent", []byte("[]"))

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'recorded-changes-issue', 'todo', 'medium', $2, 'member', 92778, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	createTask := func(status string, records ...string) string {
		t.Helper()
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, issue_id)
			VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 0, $3)
			RETURNING id
		`, agentID, status, issueID).Scan(&taskID); err != nil {
			t.Fatalf("create task: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
		for i, input := range records {
			if _, err := testPool.Exec(ctx, `
				INSERT INTO task_message (task_id, seq, type, tool, input)
				VALUES ($1, $2, 'tool_use', 'multica_commit_diff', $3::jsonb)
			`, taskID, i+1, input); err != nil {
				t.Fatalf("create change record: %v", err)
			}
		}
		return taskID
	}

	withFiles := createTask("completed",
		`{"repository":"demo","repository_id":"r","kind":"worktree","changes":[]}`,
		`{"repository":"demo","repository_id":"r","kind":"commits","changes":[{"path":"a.go","kind":"modify"}]}`,
	)
	empty := createTask("completed", `{"repository":"demo","repository_id":"r","changes":[]}`)
	failed := createTask("completed", `{"repository":"demo","repository_id":"r","unavailable":true,"changes":[]}`)
	malformed := createTask("completed", `{"repository":"demo","repository_id":"r","changes":{"path":"a.go"}}`)
	running := createTask("running")

	req := newRequest("GET", "/api/issues/"+issueID+"/task-runs?view=timeline", nil)
	req = withURLParam(req, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.ListTasksByIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp []AgentTaskResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode task list: %v", err)
	}
	got := make(map[string]bool, len(resp))
	for _, task := range resp {
		got[task.ID] = task.HasRecordedChanges
	}
	want := map[string]bool{withFiles: true, empty: false, failed: false, malformed: false, running: false}
	for id, expected := range want {
		recorded, ok := got[id]
		if !ok {
			t.Fatalf("task %s missing from response", id)
		}
		if recorded != expected {
			t.Errorf("task %s has_recorded_changes = %v, want %v", id, recorded, expected)
		}
	}
}
