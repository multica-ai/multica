package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// runs_started is what the assign-confirm dialog reports after submit, now that
// it no longer predicts the outcome with a preview round-trip (MUL-5010). These
// tests pin the wire contract directly on the write responses: the number must
// track real enqueues, and it must not leak onto read paths.
//
// Fixtures (seededReadyAgentID / createIssueForTest / taskCountFor) are shared
// with issue_trigger_preview_test.go, which covers the enqueue predicate itself.

// updateIssueRaw performs a single-issue write and returns the decoded response
// both as a typed IssueResponse and as the raw JSON object, so a test can assert
// on a field's value and on its presence/absence.
func updateIssueRaw(t *testing.T, issueID string, body map[string]any) (IssueResponse, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, body), "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	raw := w.Body.Bytes()
	var typed IssueResponse
	if err := json.Unmarshal(raw, &typed); err != nil {
		t.Fatalf("decode issue response: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("decode issue response as object: %v", err)
	}
	return typed, obj
}

func batchUpdateIssues(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues/batch-update?workspace_id="+testWorkspaceID, body)
	testHandler.BatchUpdateIssues(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("BatchUpdateIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode batch response: %v", err)
	}
	return resp
}

func wantRunsStarted(t *testing.T, resp IssueResponse, want int) {
	t.Helper()
	if resp.RunsStarted == nil {
		t.Fatalf("expected runs_started=%d on the write response, field was absent", want)
	}
	if *resp.RunsStarted != want {
		t.Fatalf("expected runs_started=%d, got %d", want, *resp.RunsStarted)
	}
}

func wantBatchCount(t *testing.T, resp map[string]any, key string, want int) {
	t.Helper()
	v, ok := resp[key]
	if !ok {
		t.Fatalf("expected %q in batch response, got %+v", key, resp)
	}
	n, ok := v.(float64)
	if !ok {
		t.Fatalf("expected %q to be a number, got %T (%+v)", key, v, v)
	}
	if int(n) != want {
		t.Fatalf("expected %s=%d, got %d", key, want, int(n))
	}
}

// TestUpdateIssueRunsStartedOne — an assign that actually enqueues reports 1,
// and the count matches the tasks really in the queue.
func TestUpdateIssueRunsStartedOne(t *testing.T) {
	agentID := seededReadyAgentID(t)
	issue := createIssueForTest(t, map[string]any{"title": "runs_started one", "status": "todo"})

	resp, _ := updateIssueRaw(t, issue.ID, map[string]any{
		"assignee_type": "agent", "assignee_id": agentID,
	})

	wantRunsStarted(t, resp, 1)
	// The number must describe reality, not the predicate: cross-check the queue.
	if got := taskCountFor(t, issue.ID, agentID); got != 1 {
		t.Fatalf("runs_started=1 but the queue holds %d tasks", got)
	}
}

// TestUpdateIssueRunsStartedZero — the two ways a single assign starts nothing.
// Both must report 0 rather than omitting the field, so the client can tell
// "no run" apart from "old backend that never sends it".
func TestUpdateIssueRunsStartedZero(t *testing.T) {
	agentID := seededReadyAgentID(t)

	t.Run("suppress_run", func(t *testing.T) {
		// The dialog's 暂不开始 path: assignee applied, deliberately no run.
		issue := createIssueForTest(t, map[string]any{"title": "runs_started zero suppressed", "status": "todo"})
		resp, _ := updateIssueRaw(t, issue.ID, map[string]any{
			"assignee_type": "agent", "assignee_id": agentID, "suppress_run": true,
		})
		wantRunsStarted(t, resp, 0)
		if got := taskCountFor(t, issue.ID, agentID); got != 0 {
			t.Fatalf("runs_started=0 but the queue holds %d tasks", got)
		}
	})

	t.Run("backlog_parks", func(t *testing.T) {
		// Backlog is the parking lot — assigning into it never starts a run.
		issue := createIssueForTest(t, map[string]any{"title": "runs_started zero backlog", "status": "backlog"})
		resp, _ := updateIssueRaw(t, issue.ID, map[string]any{
			"assignee_type": "agent", "assignee_id": agentID,
		})
		wantRunsStarted(t, resp, 0)
		if got := taskCountFor(t, issue.ID, agentID); got != 0 {
			t.Fatalf("runs_started=0 but the queue holds %d tasks", got)
		}
	})
}

// TestBatchUpdateRunsStartedAggregates — the batch reply carries the real
// enqueue total for the whole batch, which is deliberately NOT the same as
// `updated`: every issue is updated, only the eligible ones start a run.
func TestBatchUpdateRunsStartedAggregates(t *testing.T) {
	agentID := seededReadyAgentID(t)
	activeA := createIssueForTest(t, map[string]any{"title": "batch runs active A", "status": "todo"})
	activeB := createIssueForTest(t, map[string]any{"title": "batch runs active B", "status": "todo"})
	parked := createIssueForTest(t, map[string]any{"title": "batch runs parked", "status": "backlog"})

	resp := batchUpdateIssues(t, map[string]any{
		"issue_ids": []string{activeA.ID, activeB.ID, parked.ID},
		"updates":   map[string]any{"assignee_type": "agent", "assignee_id": agentID},
	})

	// 3 issues assigned, 2 runs started — the divergence is the whole point.
	wantBatchCount(t, resp, "updated", 3)
	wantBatchCount(t, resp, "runs_started", 2)

	for _, id := range []string{activeA.ID, activeB.ID} {
		if got := taskCountFor(t, id, agentID); got != 1 {
			t.Fatalf("active issue %s: expected 1 queued task, got %d", id, got)
		}
	}
	if got := taskCountFor(t, parked.ID, agentID); got != 0 {
		t.Fatalf("backlog issue should hold no task, got %d", got)
	}
}

// TestBatchUpdateRunsStartedZeroWhenSuppressed — suppress_run is batch-wide, so
// the aggregate is 0 while every issue still updates.
func TestBatchUpdateRunsStartedZeroWhenSuppressed(t *testing.T) {
	agentID := seededReadyAgentID(t)
	one := createIssueForTest(t, map[string]any{"title": "batch suppressed 1", "status": "todo"})
	two := createIssueForTest(t, map[string]any{"title": "batch suppressed 2", "status": "todo"})

	resp := batchUpdateIssues(t, map[string]any{
		"issue_ids": []string{one.ID, two.ID},
		"updates": map[string]any{
			"assignee_type": "agent", "assignee_id": agentID, "suppress_run": true,
		},
	})

	wantBatchCount(t, resp, "updated", 2)
	wantBatchCount(t, resp, "runs_started", 0)
	for _, id := range []string{one.ID, two.ID} {
		if got := taskCountFor(t, id, agentID); got != 0 {
			t.Fatalf("suppressed issue %s should hold no task, got %d", id, got)
		}
	}
}

// TestRunsStartedAbsentOnReadPaths — runs_started describes one write, so it
// must never ride along on a read. If it leaked into GET, clients caching the
// entity would keep a stale "a run started" fact attached to the issue forever.
func TestRunsStartedAbsentOnReadPaths(t *testing.T) {
	agentID := seededReadyAgentID(t)
	issue := createIssueForTest(t, map[string]any{"title": "runs_started not on reads", "status": "todo"})

	_, writeObj := updateIssueRaw(t, issue.ID, map[string]any{
		"assignee_type": "agent", "assignee_id": agentID,
	})
	if _, ok := writeObj["runs_started"]; !ok {
		t.Fatalf("write response should carry runs_started, got %+v", writeObj)
	}

	w := httptest.NewRecorder()
	testHandler.GetIssue(w, withURLParam(newRequest("GET", "/api/issues/"+issue.ID, nil), "id", issue.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("GetIssue: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var readObj map[string]any
	if err := json.NewDecoder(w.Body).Decode(&readObj); err != nil {
		t.Fatalf("decode issue read: %v", err)
	}
	if _, leaked := readObj["runs_started"]; leaked {
		t.Fatalf("runs_started must not appear on GET /api/issues/:id, got %+v", readObj["runs_started"])
	}
}
