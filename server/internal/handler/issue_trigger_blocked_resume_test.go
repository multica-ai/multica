package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Unblocking an assigned issue must start a run (AERIS-284).
//
// `blocked` is the other parking lot, and unlike `backlog` an AGENT is what
// puts issues there — the runtime brief has it write `blocked` when it gets
// stuck, ending the turn with the assignee intact and nothing queued. Nothing
// server-side takes the issue back out, so moving it to `todo` is how someone
// says "the blocker is gone, carry on". Before this fix that write landed while
// WillEnqueueRun kept its status source keyed on `backlog` alone, leaving a
// runnable-looking issue with no run behind it and `issue rerun` as the only
// way out.

// blockedIssueWithAgent builds the state a stuck run leaves behind: parked on
// `blocked`, agent assignee intact, nothing queued.
func blockedIssueWithAgent(t *testing.T, title, agentID string) string {
	t.Helper()
	issueID := dbfx.Issue(t, title, testutil.Cols{
		"status":        "blocked",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	// The runs this test provokes are inserted by the handler, so they are not
	// registered for cleanup by the fixture that made the issue.
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	return issueID
}

// putIssueStatus applies one status write through PUT /api/issues/{id} — the
// single server path behind both `multica issue status` and
// `multica issue update --status`.
func putIssueStatus(t *testing.T, issueID string, body map[string]any) {
	t.Helper()
	req := newRequest("PUT", "/api/issues/"+issueID, body)
	req = withURLParam(req, "id", issueID)
	testutil.Call(t, testHandler.UpdateIssue, req).Want(http.StatusOK)
}

// pendingTasksFor counts the runs that occupy the (issue, agent) queue slot.
func pendingTasksFor(t *testing.T, issueID, agentID string) int {
	t.Helper()
	return dbfx.Count(t,
		`SELECT count(*) FROM agent_task_queue
		 WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued', 'dispatched')`,
		issueID, agentID)
}

func TestBlockedToTodoEnqueuesForAgentAssignee(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Blocked Resume Agent", nil)
	issueID := blockedIssueWithAgent(t, "Unblock me", agentID)

	putIssueStatus(t, issueID, map[string]any{"status": "todo"})

	if got := pendingTasksFor(t, issueID, agentID); got != 1 {
		t.Fatalf("blocked → todo must enqueue exactly 1 run for the agent assignee, got %d", got)
	}
}

// `issue update --status` shares this handler with `issue status`, so a status
// carried alongside other fields must resume identically.
func TestBlockedToTodoEnqueuesWhenStatusRidesWithOtherFields(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Blocked Resume Update Agent", nil)
	issueID := blockedIssueWithAgent(t, "Unblock me with a priority bump", agentID)

	putIssueStatus(t, issueID, map[string]any{"status": "todo", "priority": "high"})

	if got := pendingTasksFor(t, issueID, agentID); got != 1 {
		t.Fatalf("issue update --status must resume the same way, got %d pending tasks", got)
	}
}

// --no-start sends suppress_run: the status lands with no run behind it.
func TestBlockedToTodoWithSuppressRunDoesNotEnqueue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Blocked Resume NoStart Agent", nil)
	issueID := blockedIssueWithAgent(t, "Unblock without starting", agentID)

	putIssueStatus(t, issueID, map[string]any{"status": "todo", "suppress_run": true})

	if got := pendingTasksFor(t, issueID, agentID); got != 0 {
		t.Fatalf("suppress_run must not enqueue, got %d pending tasks", got)
	}
}

// Re-blocking and resuming again coalesces onto the run already waiting, the
// same (issue_id, agent_id) pending guard the backlog promotion path uses.
func TestRepeatedResumeDoesNotStackTasks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	agentID := createHandlerTestAgent(t, "Blocked Resume Dedup Agent", nil)
	issueID := blockedIssueWithAgent(t, "Unblock me twice", agentID)

	putIssueStatus(t, issueID, map[string]any{"status": "todo"})
	// Blocking again needs no suppress_run — a move INTO blocked never enqueues.
	putIssueStatus(t, issueID, map[string]any{"status": "blocked"})
	putIssueStatus(t, issueID, map[string]any{"status": "todo"})

	if got := pendingTasksFor(t, issueID, agentID); got != 1 {
		t.Fatalf("a second resume must not stack a task, got %d pending tasks", got)
	}
}

// Only `todo` resumes a blocked issue. The other targets each fail for their
// own reason, and this is the whole set:
//
//   - `in_progress` / `in_review` report on work already in flight. An agent
//     woken on a blocked issue by an @mention writes `in_progress` itself, so
//     resuming on it would let a run enqueue its own successor. This is the
//     case that keeps `blocked` from collapsing into the backlog rule.
//   - `backlog` / `done` / `cancelled` park or close the issue.
func TestBlockedToNonTodoStatusDoesNotEnqueue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	for _, target := range []string{"in_progress", "in_review", "backlog", "done", "cancelled"} {
		t.Run(target, func(t *testing.T) {
			agentID := createHandlerTestAgent(t, "Blocked To "+target+" Agent", nil)
			issueID := blockedIssueWithAgent(t, "Unblock into "+target, agentID)

			putIssueStatus(t, issueID, map[string]any{"status": target})

			if got := pendingTasksFor(t, issueID, agentID); got != 0 {
				t.Fatalf("blocked → %s must not enqueue, got %d pending tasks", target, got)
			}
		})
	}
}

// A human assignee has no runtime to wake, and an unassigned issue has nobody
// to run it; unblocking either is a plain status write.
func TestBlockedToTodoWithoutAgentAssigneeDoesNotEnqueue(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	cases := []struct {
		name string
		cols testutil.Cols
	}{
		{"member_assignee", testutil.Cols{
			"status": "blocked", "assignee_type": "member", "assignee_id": testUserID,
		}},
		{"unassigned", testutil.Cols{"status": "blocked"}},
	}
	for _, tc := range cases {
		name := tc.name
		t.Run(name, func(t *testing.T) {
			issueID := dbfx.Issue(t, "Unblock "+name, tc.cols)
			dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)

			putIssueStatus(t, issueID, map[string]any{"status": "todo"})

			if got := dbfx.Count(t,
				`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID); got != 0 {
				t.Fatalf("resuming a %s issue must not enqueue, got %d tasks", name, got)
			}
		})
	}
}

// A squad-assigned issue resumes through the same leader routing the backlog
// promotion uses — the task belongs to the leader, not to the squad.
func TestBlockedToTodoEnqueuesSquadLeader(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	leaderID := createHandlerTestAgent(t, "Blocked Resume Squad Leader", nil)
	squadID := dbfx.Squad(t, "Blocked Resume Squad", leaderID)
	issueID := dbfx.Issue(t, "Squad is blocked", testutil.Cols{
		"status":        "blocked",
		"assignee_type": "squad",
		"assignee_id":   squadID,
	})
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)

	putIssueStatus(t, issueID, map[string]any{"status": "todo"})

	if got := pendingTasksFor(t, issueID, leaderID); got != 1 {
		t.Fatalf("blocked → todo must enqueue exactly 1 run for the squad leader, got %d", got)
	}
}
