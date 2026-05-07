package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBatchUpdateNoMutationReturnsZero — regression for #1660.
//
// When the request payload has valid issue_ids but the "updates" field
// is empty, missing, or doesn't decode any known mutation field, the
// handler used to walk every issue, run a no-op UPDATE, and increment
// `updated` for each one — returning {"updated": N} despite changing
// nothing. Reporters saw 200 + a positive count and assumed the call
// worked, then chased a phantom persistence bug.
//
// The fix is "tell the truth": when no mutation field is present, return
// {"updated": 0} immediately so the count matches reality.
func TestBatchUpdateNoMutationReturnsZero(t *testing.T) {
	// Two fresh issues so we can also assert no fields actually changed.
	a := createTestIssue(t, "BU-no-mut A", "todo", "low")
	b := createTestIssue(t, "BU-no-mut B", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	t.Cleanup(func() { deleteTestIssue(t, b) })

	cases := []struct {
		desc string
		body map[string]any
	}{
		{
			desc: "updates_missing",
			// Most common reporter pattern: status at top level.
			body: map[string]any{"issue_ids": []string{a, b}, "status": "in_progress"},
		},
		{
			desc: "updates_empty_object",
			body: map[string]any{"issue_ids": []string{a, b}, "updates": map[string]any{}},
		},
		{
			desc: "updates_misnamed",
			// Singular "update" instead of plural "updates".
			body: map[string]any{"issue_ids": []string{a, b}, "update": map[string]any{"status": "done"}},
		},
		{
			desc: "updates_unknown_field_only",
			// Payload IS nested correctly, but every key inside `updates` is
			// unknown to the handler — same class of caller mistake as the
			// shapes above. hasMutation must stay false; behavior is already
			// correct, this case locks it in against future regressions.
			body: map[string]any{"issue_ids": []string{a, b}, "updates": map[string]any{"foo": "bar"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/issues/batch-update", tc.body)
			testHandler.BatchUpdateIssues(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
			}
			var resp struct {
				Updated int `json:"updated"`
			}
			json.NewDecoder(w.Body).Decode(&resp)
			if resp.Updated != 0 {
				t.Errorf("expected updated=0 when no mutation field present, got %d", resp.Updated)
			}

			// Belt and braces: confirm the issues weren't touched.
			for _, id := range []string{a, b} {
				gw := httptest.NewRecorder()
				gr := newRequest("GET", "/api/issues/"+id, nil)
				gr = withURLParam(gr, "id", id)
				testHandler.GetIssue(gw, gr)
				var got IssueResponse
				json.NewDecoder(gw.Body).Decode(&got)
				if got.Status != "todo" {
					t.Errorf("issue %s: status changed to %q despite no-mutation request", id, got.Status)
				}
			}
		})
	}
}

// TestBatchUpdateValidUpdatesPersistAndCount — positive case to lock in
// happy-path behavior alongside the regression test above.
func TestBatchUpdateValidUpdatesPersistAndCount(t *testing.T) {
	a := createTestIssue(t, "BU-ok A", "todo", "low")
	b := createTestIssue(t, "BU-ok B", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, a) })
	t.Cleanup(func() { deleteTestIssue(t, b) })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{a, b},
		"updates":   map[string]any{"status": "in_progress"},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Updated int `json:"updated"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Updated != 2 {
		t.Errorf("expected updated=2, got %d", resp.Updated)
	}
	for _, id := range []string{a, b} {
		gw := httptest.NewRecorder()
		gr := newRequest("GET", "/api/issues/"+id, nil)
		gr = withURLParam(gr, "id", id)
		testHandler.GetIssue(gw, gr)
		var got IssueResponse
		json.NewDecoder(gw.Body).Decode(&got)
		if got.Status != "in_progress" {
			t.Errorf("issue %s: expected status=in_progress, got %q", id, got.Status)
		}
	}
}

func TestUpdateIssueArchiveCancelsActiveTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := createTestIssue(t, "archive-cancel-single", "in_progress", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	taskID := createIssueTask(t, issueID, "running")

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "archive"})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue archive: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	if got := taskStatus(t, ctx, taskID); got != "cancelled" {
		t.Fatalf("archive status should cancel active task, got %q", got)
	}
}

func TestBatchUpdateArchiveCancelsActiveTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID := createTestIssue(t, "archive-cancel-batch", "in_progress", "medium")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	taskID := createIssueTask(t, issueID, "dispatched")

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issueID},
		"updates":   map[string]any{"status": "archive"},
	})
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues archive: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Updated int `json:"updated"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Updated != 1 {
		t.Fatalf("expected updated=1, got %d", resp.Updated)
	}

	if got := taskStatus(t, ctx, taskID); got != "cancelled" {
		t.Fatalf("batch archive status should cancel active task, got %q", got)
	}
}

// createTestIssue is a small helper to keep the table-driven cases clean.
// Returns the new issue's id; caller is responsible for cleanup.
func createTestIssue(t *testing.T, title, status, priority string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":    title,
		"status":   status,
		"priority": priority,
	})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateIssue %q: expected 201, got %d: %s", title, w.Code, w.Body.String())
	}
	var issue IssueResponse
	json.NewDecoder(w.Body).Decode(&issue)
	return issue.ID
}

func deleteTestIssue(t *testing.T, id string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/issues/"+id, nil)
	req = withURLParam(req, "id", id)
	testHandler.DeleteIssue(w, req)
}

func createIssueTask(t *testing.T, issueID string, status string) string {
	t.Helper()
	ctx := context.Background()

	agentID := createHandlerTestAgent(t, "archive-task-agent", nil)
	var runtimeID string
	if err := testPool.QueryRow(ctx, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID); err != nil {
		t.Fatalf("load agent runtime: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		 VALUES ($1, $2, $3, $4, 0)
		 RETURNING id`,
		agentID, runtimeID, issueID, status,
	).Scan(&taskID); err != nil {
		t.Fatalf("create issue task: %v", err)
	}

	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	return taskID
}

func taskStatus(t *testing.T, ctx context.Context, taskID string) string {
	t.Helper()
	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("load task status: %v", err)
	}
	return status
}
