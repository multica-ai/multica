package handler

import (
	"net/http"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestIssueReviewReworkTrigger(t *testing.T) {
	agentID := seededReadyAgentID(t)
	for _, tc := range []struct {
		name       string
		taskStatus string
		squad      bool
		suppress   bool
		wantRun    bool
	}{
		{name: "completed", taskStatus: "completed", wantRun: true},
		{name: "queued", taskStatus: "queued"},
		{name: "dispatched", taskStatus: "dispatched"},
		{name: "running", taskStatus: "running"},
		{name: "waiting_local_directory", taskStatus: "waiting_local_directory"},
		{name: "suppressed", taskStatus: "completed", suppress: true},
		{name: "squad_completed", taskStatus: "completed", squad: true, wantRun: true},
		{name: "squad_running", taskStatus: "running", squad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assigneeType, assigneeID := "agent", agentID
			if tc.squad {
				assigneeType = "squad"
				assigneeID = dbfx.Squad(t, "Rework squad", agentID)
			}
			issueID := dbfx.Issue(t, "Review rework", testutil.Cols{
				"status": "in_review", "assignee_type": assigneeType, "assignee_id": assigneeID,
			})
			dbfx.Task(t, agentID, testutil.Cols{
				"issue_id": issueID, "runtime_id": testRuntimeID, "status": tc.taskStatus,
			})
			// Enqueued tasks are not fixture rows.
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)

			req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{
				"description": "Please repair the review findings.",
			}), "id", issueID)
			testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)
			if got := taskCountFor(t, issueID, agentID); got != 1 {
				t.Fatalf("description update created a run: got %d tasks", got)
			}

			preview := previewIssueTrigger(t, map[string]any{"issue_ids": []string{issueID}, "status": "todo"})
			wantPreview := 0
			if tc.taskStatus == "completed" {
				wantPreview = 1
			}
			if preview.TotalCount != wantPreview {
				t.Fatalf("rework preview = %+v, want %d triggers", preview, wantPreview)
			}
			if wantPreview == 1 && (preview.Triggers[0].AgentID != agentID || preview.Triggers[0].Source != "status") {
				t.Fatalf("unexpected rework trigger: %+v", preview.Triggers[0])
			}

			for attempt := range 2 {
				req = withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{
					"status": "todo", "suppress_run": tc.suppress,
				}), "id", issueID)
				var got struct {
					IssueResponse
					RunEnqueued bool `json:"run_enqueued"`
				}
				testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK).JSON(&got)
				if got.RunEnqueued != (tc.wantRun && attempt == 0) {
					t.Fatalf("run_enqueued = %v on attempt %d, want %v", got.RunEnqueued, attempt, tc.wantRun && attempt == 0)
				}
				if got.Status != "todo" || got.AssigneeType == nil || *got.AssigneeType != assigneeType ||
					got.AssigneeID == nil || *got.AssigneeID != assigneeID {
					t.Fatalf("rework changed ownership or missed status: %+v", got)
				}
			}
			wantTasks := 1
			if tc.wantRun {
				wantTasks++
			}
			if got := taskCountFor(t, issueID, agentID); got != wantTasks {
				t.Fatalf("rework task count = %d, want %d", got, wantTasks)
			}
		})
	}
}

func TestIssueReviewReworkConcurrentUpdates(t *testing.T) {
	agentID := seededReadyAgentID(t)
	issueID := dbfx.Issue(t, "Concurrent review rework", testutil.Cols{
		"status": "in_review", "assignee_type": "agent", "assignee_id": agentID,
	})
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	var writes sync.WaitGroup
	start := make(chan struct{})
	for range 4 {
		writes.Go(func() {
			<-start
			req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{
				"status": "todo",
			}), "id", issueID)
			testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)
		})
	}
	close(start)
	writes.Wait()
	if got := taskCountFor(t, issueID, agentID); got != 1 {
		t.Fatalf("concurrent rework created %d tasks, want 1", got)
	}
}

func TestBatchIssueReviewReworkTrigger(t *testing.T) {
	agentID := seededReadyAgentID(t)
	for _, suppress := range []bool{false, true} {
		issueID := dbfx.Issue(t, "Batch review rework", testutil.Cols{
			"status": "in_review", "assignee_type": "agent", "assignee_id": agentID,
		})
		dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		for range 2 {
			req := newRequest("POST", "/api/issues/batch-update", map[string]any{
				"issue_ids": []string{issueID},
				"updates":   map[string]any{"status": "todo", "suppress_run": suppress},
			})
			testutil.Call(t, testHandler.BatchUpdateIssues, req).Want(http.StatusOK)
		}
		want := 1
		if suppress {
			want = 0
		}
		if got := taskCountFor(t, issueID, agentID); got != want {
			t.Fatalf("batch rework with suppress_run=%v created %d tasks, want %d", suppress, got, want)
		}
	}
}
