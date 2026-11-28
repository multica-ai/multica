package handler

import (
	"context"
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

// The caller-filtered activity series must come from the server-side roll-up,
// not from whatever task pages the client has loaded: a member whose runs fall
// outside the first page still sees their complete 30-day counts.
func TestWorkspaceAgentActivity30dOriginatorFilter(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	if testPool == nil {
		t.Skip("database pool not available")
	}

	var runtimeID, agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID)
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)

	var issueID string
	dbfx.QueryRow(t, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'activity originator filter test', $2, 'member',
		        (SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	const callerA = "5b1f6e1a-0000-4000-8000-0000000000c3"
	const callerB = "5b1f6e1a-0000-4000-8000-0000000000d4"
	mkTask := func(caller string) {
		dbfx.Task(t, agentID, testutil.Cols{
			"issue_id":             issueID,
			"runtime_id":           runtimeID,
			"status":               "completed",
			"originator_user_id":   caller,
			"accountable_user_id":  caller,
			"started_at":           testutil.Raw("now() - interval '5 minutes'"),
			"completed_at":         testutil.Raw("now()"),
			"created_at":           testutil.Raw("now()"),
		})
	}
	mkTask(callerA)
	mkTask(callerA)
	mkTask(callerB)

	readCounts := func(originator string) int {
		path := "/api/agent-activity-30d"
		if originator != "" {
			path += "?originator_user_id=" + originator
		}
		req := withChatTestWorkspaceCtx(t, newRequest(http.MethodGet, path, nil))
		var rows []struct {
			AgentID   string `json:"agent_id"`
			TaskCount int    `json:"task_count"`
		}
		testutil.Call(t, testHandler.GetWorkspaceAgentActivity30d, req).Want(http.StatusOK).JSON(&rows)
		total := 0
		for _, row := range rows {
			if row.AgentID == agentID {
				total += row.TaskCount
			}
		}
		return total
	}

	if got := readCounts(callerA); got != 2 {
		t.Errorf("caller A task_count = %d, want 2", got)
	}
	if got := readCounts(callerB); got != 1 {
		t.Errorf("caller B task_count = %d, want 1", got)
	}
	if got := readCounts(""); got != 3 {
		t.Errorf("unfiltered task_count = %d, want 3", got)
	}
}
