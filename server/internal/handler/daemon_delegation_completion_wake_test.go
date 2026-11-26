package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The completion-side delegated wake (#9720 rework, #10060): the zero-route
// check runs on the daemon's terminal completion callback, not at comment
// time — agents post their closing comment 7–26s before the task is marked
// completed, so a comment-time check gated on a completed worker never fires.

// delegatedWakeSeed is a leader→worker delegation pair on one issue (#9720).
type delegatedWakeSeed struct {
	leaderAgent       string
	leaderTask        string
	workerAgent       string
	workerTask        string
	delegationComment string // empty when no leader delegation comment exists
}

// seedDelegationPair seeds a leader agent with a delegator task and a worker
// agent whose task is delegated_from the leader's task. When
// withDelegationComment is set, the leader's delegation comment exists and is
// the worker task's trigger, so worker replies thread under it.
func seedDelegationPair(t *testing.T, issueID, leaderStatus, workerStatus string, withDelegationComment bool, extraWorkerCols ...testutil.Cols) delegatedWakeSeed {
	t.Helper()
	leaderAgent := dbfx.Agent(t, "9720 wake coordinator", testRuntimeID)
	workerAgent := dbfx.Agent(t, "9720 wake worker", testRuntimeID)
	leaderTask := dbfx.Task(t, leaderAgent, testutil.Cols{
		"runtime_id": testRuntimeID, "issue_id": issueID, "status": leaderStatus,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	var delegationComment string
	workerCols := testutil.Cols{
		"runtime_id": testRuntimeID, "issue_id": issueID, "status": workerStatus,
		"delegated_from_task_id": leaderTask,
		"originator_user_id":     testUserID, "accountable_user_id": testUserID,
	}
	if withDelegationComment {
		delegationComment = dbfx.Comment(t, issueID, "please handle this part", testutil.Cols{
			"author_type": "agent", "author_id": leaderAgent, "source_task_id": leaderTask,
		})
		workerCols["trigger_comment_id"] = delegationComment
	}
	for _, extra := range extraWorkerCols {
		for k, v := range extra {
			workerCols[k] = v
		}
	}
	workerTask := dbfx.Task(t, workerAgent, workerCols)
	dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id = $1`, issueID)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	return delegatedWakeSeed{
		leaderAgent: leaderAgent, leaderTask: leaderTask,
		workerAgent: workerAgent, workerTask: workerTask,
		delegationComment: delegationComment,
	}
}

// postCommentAsAgent posts a comment through CreateComment in the worker's
// agent identity (the X-Agent-ID + X-Task-ID pair resolveActor trusts).
func postCommentAsAgent(t *testing.T, issueID, agentID, taskID, content, parentID string) (*httptest.ResponseRecorder, string) {
	t.Helper()
	body := map[string]any{"content": content}
	if parentID != "" {
		body["parent_id"] = parentID
	}
	r := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", body)
	r.Header.Set("X-Agent-ID", agentID)
	r.Header.Set("X-Task-ID", taskID)
	r = withURLParam(r, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.CreateComment(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateComment: %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("decode created comment: %v (%s)", err, w.Body.String())
	}
	return w, resp.ID
}

// tasksCarryingComment counts leader-agent tasks that were triggered or
// coalesced by the given comment — the wake/mention receipt either way.
func tasksCarryingComment(t *testing.T, issueID, agentID, commentID string) int {
	t.Helper()
	return dbfx.Count(t, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
		  AND (trigger_comment_id = $3::uuid OR $3::uuid = ANY(coalesced_comment_ids))`,
		issueID, agentID, commentID)
}

// completionWakeCount counts dedicated delegated_completion wake tasks for one
// worker task (#9720's one-shot evidence).
func completionWakeCount(t *testing.T, workerTaskID string) int {
	t.Helper()
	return dbfx.Count(t, `
		SELECT count(*) FROM agent_task_queue
		WHERE trigger_evidence_kind = 'delegated_completion' AND trigger_evidence_ref_id = $1::uuid`,
		workerTaskID)
}

// seedWakeIssue creates an in-progress issue assigned to the leader agent —
// the single-agent assignment whose only routing hope for an un-mentioning
// worker reply is the completion wake.
func seedWakeIssue(t *testing.T, title string) string {
	t.Helper()
	issue := createIssueForTest(t, map[string]any{"title": title, "status": "in_progress"})
	return issue.ID
}

// TestCompleteTask_ZeroRouteClosingCommentWakesDelegator is the ordering
// regression DonYum's production data demanded: the worker posts its closing
// comment while still running (7–26s before the flip, always), and the wake
// fires exactly once when the completion transition wins — not at comment
// time.
func TestCompleteTask_ZeroRouteClosingCommentWakesDelegator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 completion-side wake")
	seed := seedDelegationPair(t, issueID, "running", "running", true)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)

	// The worker posts its closing report before completing: zero triggers at
	// comment time, and (until the flip) no wake.
	_, replyID := postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
		"closing report: done, no mention", seed.delegationComment)
	if got := completionWakeCount(t, seed.workerTask); got != 0 {
		t.Fatalf("wake tasks before completion = %d, want 0 (comment-time check must not fire)", got)
	}

	if w := completeTaskViaHandler(t, seed.workerTask, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: %d: %s", w.Code, w.Body.String())
	}

	if got := tasksCarryingComment(t, issueID, seed.leaderAgent, replyID); got != 1 {
		t.Fatalf("leader tasks carrying the closing comment = %d, want 1 (delegator not woken)", got)
	}
	if got := completionWakeCount(t, seed.workerTask); got != 1 {
		t.Fatalf("delegated_completion wake tasks = %d, want 1", got)
	}

	// A daemon replay of the terminal callback must not wake again.
	if w := completeTaskViaHandler(t, seed.workerTask, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask replay: %d: %s", w.Code, w.Body.String())
	}
	if got := completionWakeCount(t, seed.workerTask); got != 1 {
		t.Fatalf("wake tasks after replay = %d, want 1", got)
	}
}

// TestCompleteTask_WakeChainCapsAtOneHop pins the ping-pong guards: a
// coordinator run spawned as a completion wake (A2) never hands control
// further up, whether it completes zero-route (case 1) or delegates onward to
// B whose own zero-route completion would bounce back through A2 (case 2).
func TestCompleteTask_WakeChainCapsAtOneHop(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 one-hop cap")

	// A (leader, running) delegated to B, which completed zero-route; the wake
	// A2 exists and carries the delegated_completion evidence kind.
	seed := seedDelegationPair(t, issueID, "running", "completed", true)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)
	dbfx.Exec(t, `INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority,
			originator_user_id, accountable_user_id, delegated_from_task_id, trigger_evidence_kind, trigger_evidence_ref_id)
		VALUES ($1, $2, $3, 'queued', 0, $4, $4, $5, 'delegated_completion', $5)`,
		seed.leaderAgent, testRuntimeID, issueID, testUserID, seed.workerTask)
	var a2Task string
	dbfx.QueryRow(t, `SELECT id::text FROM agent_task_queue
		WHERE agent_id = $1 AND trigger_evidence_kind = 'delegated_completion' LIMIT 1`, seed.leaderAgent).Scan(&a2Task)
	if a2Task == "" {
		t.Fatal("wake task A2 not seeded")
	}

	// Case 1: A2 is the coordinator of a fresh delegation to B; B completes
	// zero-route. The would-be target A2 carries the completion evidence kind,
	// so nothing wakes.
	bAgent := dbfx.Agent(t, "9720 one-hop worker B", testRuntimeID)
	bTask := dbfx.Task(t, bAgent, testutil.Cols{
		"runtime_id": testRuntimeID, "issue_id": issueID, "status": "completed",
		"delegated_from_task_id": a2Task,
		"originator_user_id":     testUserID, "accountable_user_id": testUserID,
	})
	dbfx.Comment(t, issueID, "B closing report, no mention", testutil.Cols{
		"author_type": "agent", "author_id": bAgent, "source_task_id": bTask,
	})
	if w := completeTaskViaHandler(t, bTask, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask B: %d: %s", w.Code, w.Body.String())
	}
	if got := completionWakeCount(t, bTask); got != 0 {
		t.Fatalf("wake tasks for B = %d, want 0 (propagation stops at the wake run)", got)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status IN ('queued','dispatched')`,
		issueID, seed.leaderAgent); got != 1 {
		t.Fatalf("leader pending tasks = %d, want 1 (only the original wake A2)", got)
	}
}

// TestCompleteTask_RunWithNoOwnCommentDoesNotWake pins the boundary with the
// platform fallback comment (#8922's territory): a delegated worker that
// posted no agent-authored comment of its own is left alone.
func TestCompleteTask_RunWithNoOwnCommentDoesNotWake(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 no own comment")
	seed := seedDelegationPair(t, issueID, "running", "running", false)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)

	if w := completeTaskViaHandler(t, seed.workerTask, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: %d: %s", w.Code, w.Body.String())
	}
	if got := completionWakeCount(t, seed.workerTask); got != 0 {
		t.Fatalf("wake tasks = %d, want 0 (no agent-authored comment of its own)", got)
	}
}

// TestCompleteTask_MentionedClosingCommentDoesNotWake pins that an explicit
// mention keeps its existing routing and never double-fires the wake.
func TestCompleteTask_MentionedClosingCommentDoesNotWake(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 mention no wake")
	seed := seedDelegationPair(t, issueID, "running", "running", true)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)

	var leaderName string
	dbfx.QueryRow(t, `SELECT name FROM agent WHERE id = $1`, seed.leaderAgent).Scan(&leaderName)
	_, replyID := postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
		"done — ["+leaderName+"](mention://agent/"+seed.leaderAgent+") please review", seed.delegationComment)

	if w := completeTaskViaHandler(t, seed.workerTask, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: %d: %s", w.Code, w.Body.String())
	}
	if got := tasksCarryingComment(t, issueID, seed.leaderAgent, replyID); got != 1 {
		t.Fatalf("leader tasks carrying the mentioned reply = %d, want exactly 1 (mention path)", got)
	}
	if got := completionWakeCount(t, seed.workerTask); got != 0 {
		t.Fatalf("delegated_completion wake tasks = %d, want 0 (mention must not double-wake)", got)
	}
}

// TestCompleteTask_ZeroRouteCommentFoldsIntoPendingCoordinator pins the
// coalescing behavior: when the delegator already owns a pending task, the
// wake folds into it instead of creating a second run.
func TestCompleteTask_ZeroRouteCommentFoldsIntoPendingCoordinator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 fold into pending")
	seed := seedDelegationPair(t, issueID, "queued", "running", true)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)
	// The pending coordinator task is comment-triggered on the delegation
	// thread, so the fold's thread match applies (the INSERT trigger stamps
	// comment_thread_id; the UPDATE restates it).
	dbfx.Exec(t, `UPDATE agent_task_queue SET trigger_comment_id = $2, comment_thread_id = comment_thread_root_id($2) WHERE id = $1`,
		seed.leaderTask, seed.delegationComment)

	_, replyID := postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
		"closing report: done, no mention", seed.delegationComment)
	if w := completeTaskViaHandler(t, seed.workerTask, "done"); w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: %d: %s", w.Code, w.Body.String())
	}

	if got := tasksCarryingComment(t, issueID, seed.leaderAgent, replyID); got != 1 {
		t.Fatalf("leader tasks carrying the comment = %d, want 1 (folded into the pending task)", got)
	}
	if got := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`,
		issueID, seed.leaderAgent); got != 1 {
		t.Fatalf("leader tasks total = %d, want 1 (fold, no second run)", got)
	}
	if got := completionWakeCount(t, seed.workerTask); got != 0 {
		t.Fatalf("standalone wake tasks = %d, want 0 (fold does not create a row)", got)
	}
}

// TestCompleteTask_IneligibleDelegatorDoesNotWake covers the guard cluster:
// an archived delegator, a delegator without a runtime, and an autopilot-run
// worker all owe no wake.
func TestCompleteTask_IneligibleDelegatorDoesNotWake(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	cases := []struct {
		name       string
		leaderCols testutil.Cols
		workerCols testutil.Cols
	}{
		{
			name:       "archived delegator",
			leaderCols: testutil.Cols{"archived_at": testutil.Raw("now()")},
		},
		{
			name:       "delegator without runtime",
			leaderCols: testutil.Cols{"runtime_id": nil},
		},
		{
			name: "autopilot worker",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issueID := seedWakeIssue(t, "9720 ineligible "+tc.name)
			seed := seedDelegationPair(t, issueID, "running", "running", true)
			dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)
			for k, v := range tc.leaderCols {
				dbfx.Exec(t, `UPDATE agent SET `+k+` = $2 WHERE id = $1`, seed.leaderAgent, v)
			}
			for k, v := range tc.workerCols {
				dbfx.Exec(t, `UPDATE agent_task_queue SET `+k+` = $2 WHERE id = $1`, seed.workerTask, v)
			}
			if tc.name == "autopilot worker" {
				var autopilotID, runID string
				dbfx.QueryRow(t, `
					INSERT INTO autopilot (workspace_id, title, assignee_type, assignee_id, status, execution_mode, created_by_type, created_by_id)
					VALUES ($1, 'wake ap', 'agent', $2, 'active', 'run_only', 'member', $3) RETURNING id`,
					testWorkspaceID, seed.workerAgent, testUserID).Scan(&autopilotID)
				dbfx.QueryRow(t, `INSERT INTO autopilot_run (autopilot_id, source, status) VALUES ($1, 'manual', 'running') RETURNING id`,
					autopilotID).Scan(&runID)
				dbfx.Exec(t, `UPDATE agent_task_queue SET autopilot_run_id = $2 WHERE id = $1`, seed.workerTask, runID)
			}

			postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
				"closing report: done, no mention", seed.delegationComment)
			if w := completeTaskViaHandler(t, seed.workerTask, "done"); w.Code != http.StatusOK {
				t.Fatalf("CompleteTask: %d: %s", w.Code, w.Body.String())
			}
			if got := completionWakeCount(t, seed.workerTask); got != 0 {
				t.Fatalf("wake tasks = %d, want 0", got)
			}
		})
	}
}
