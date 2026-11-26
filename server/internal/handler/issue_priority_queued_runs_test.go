package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func queuedRunPriority(t *testing.T, taskID string) int {
	t.Helper()
	var priority int
	dbfx.QueryRow(t, `SELECT priority FROM agent_task_queue WHERE id = $1`, taskID).Scan(&priority)
	return priority
}

// Editing an issue's priority moves its waiting runs with it, so raising a
// starved issue also raises its queued runs in the claim order (GH #11621).
func TestIssuePriorityEditMovesWaitingRuns(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "priority-sync runtime")
	issueID := dbfx.Issue(t, "priority-sync issue", testutil.Cols{"priority": "low"})

	// One pending run per (issue, agent), so each run gets its own agent.
	run := func(status string, extra ...testutil.Cols) string {
		agentID := dbfx.Agent(t, "priority-sync agent "+status, runtimeID)
		cols := testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": status, "priority": 1}
		for _, c := range extra {
			for k, v := range c {
				cols[k] = v
			}
		}
		return dbfx.Task(t, agentID, cols)
	}
	queued := run("queued")
	deferred := run("deferred", testutil.Cols{"fire_at": testutil.Raw("now() + interval '1 hour'")})
	running := run("running", testutil.Cols{"started_at": testutil.Raw("now()")})
	chatAgent := dbfx.Agent(t, "priority-sync chat agent", runtimeID)
	chat := dbfx.Task(t, chatAgent, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "queued", "priority": 3,
		"chat_session_id": dbfx.ChatSession(t, chatAgent),
	})

	req := newRequest(http.MethodPatch, "/api/issues/"+issueID, map[string]any{"priority": "urgent"})
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(req, "id", issueID)).Want(http.StatusOK)

	for name, tc := range map[string]struct {
		id   string
		want int
	}{
		"queued":   {queued, 4},
		"deferred": {deferred, 4},
		"running":  {running, 1},
		"chat":     {chat, 3},
	} {
		if got := queuedRunPriority(t, tc.id); got != tc.want {
			t.Errorf("after single edit, %s run priority = %d, want %d", name, got, tc.want)
		}
	}

	batch := newRequest(http.MethodPost, "/api/issues/batch-update", map[string]any{
		"issue_ids": []string{issueID},
		"updates":   map[string]any{"priority": "medium"},
	})
	testutil.Call(t, testHandler.BatchUpdateIssues, batch).Want(http.StatusOK)

	if got := queuedRunPriority(t, queued); got != 2 {
		t.Errorf("after batch edit, queued run priority = %d, want 2", got)
	}
	if got := queuedRunPriority(t, chat); got != 3 {
		t.Errorf("after batch edit, chat run priority = %d, want 3", got)
	}
}
