package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestAgentActivityOutcomes(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "activity outcomes runtime")
	agentID := dbfx.Agent(t, "activity outcomes agent", runtimeID)
	issueID := dbfx.Issue(t, "activity outcomes issue")

	// Exercise the cancellation handler, including a run stopped after starting.
	for i := 0; i < 8; i++ {
		cols := testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID}
		if i == 7 {
			cols["status"] = "running"
			cols["started_at"] = testutil.Raw("now() - interval '1 minute'")
		}
		taskID := dbfx.Task(t, agentID, cols)
		testutil.Call(t, testHandler.CancelTaskByUser, cancelTaskByUserRequest(t, testUserID, taskID)).Want(http.StatusOK)
		if got := taskStatus(t, taskID); got != "cancelled" {
			t.Fatalf("cancel handler left task %s in %s", taskID, got)
		}
	}

	readCounts := func(t *testing.T, wantTotal, wantCompleted, wantFailed, wantCancelled int) {
		t.Helper()
		var rows []struct {
			AgentID        string `json:"agent_id"`
			TaskCount      int    `json:"task_count"`
			CompletedCount *int   `json:"completed_count"`
			FailedCount    int    `json:"failed_count"`
			CancelledCount *int   `json:"cancelled_count"`
		}
		req := withChatTestWorkspaceCtx(t, newRequest(http.MethodGet, "/api/agent-activity-30d", nil))
		testutil.Call(t, testHandler.GetWorkspaceAgentActivity30d, req).Want(http.StatusOK).JSON(&rows)
		var total, completed, failed, cancelled int
		for _, row := range rows {
			if row.AgentID != agentID {
				continue
			}
			if row.CompletedCount == nil || row.CancelledCount == nil {
				t.Fatalf("activity response must distinguish completed/cancelled counts: %+v", row)
			}
			total += row.TaskCount
			completed += *row.CompletedCount
			failed += row.FailedCount
			cancelled += *row.CancelledCount
		}
		if total != wantTotal || completed != wantCompleted || failed != wantFailed || cancelled != wantCancelled {
			t.Fatalf("total/completed/failed/cancelled = %d/%d/%d/%d, want %d/%d/%d/%d", total, completed, failed, cancelled, wantTotal, wantCompleted, wantFailed, wantCancelled)
		}
	}

	t.Run("only_cancelled", func(t *testing.T) { readCounts(t, 8, 0, 0, 8) })
	for _, status := range []string{"completed", "failed"} {
		dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id":   runtimeID,
			"issue_id":     issueID,
			"status":       status,
			"started_at":   testutil.Raw("now() - interval '1 minute'"),
			"completed_at": testutil.Raw("now()"),
		})
	}
	t.Run("mixed_outcomes", func(t *testing.T) { readCounts(t, 10, 1, 1, 8) })

	// Neither old outcomes nor a still-running attempt belongs in the window.
	for _, status := range []string{"completed", "failed", "cancelled"} {
		dbfx.Task(t, agentID, testutil.Cols{
			"status":       status,
			"completed_at": testutil.Raw("now() - interval '31 days'"),
		})
	}
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	})
	t.Run("window_and_active_runs", func(t *testing.T) { readCounts(t, 10, 1, 1, 8) })
}
