package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// delegatedWakeSeed is a leader→worker delegation pair on one issue (#9720):
// the leader's task delegated to the worker's task, and the worker has since
// completed its run.
type delegatedWakeSeed struct {
	leaderAgent       string
	leaderTask        string
	workerAgent       string
	workerTask        string
	delegationComment string // empty when no leader delegation comment exists
}

// seedDelegationPair seeds a leader agent with a delegator task in leaderStatus
// and a worker agent whose task (workerStatus, 'completed' for the #9720 shape)
// is delegated_from the leader's task. When withDelegationComment is set, the
// leader's delegation comment exists and is the worker task's trigger, so
// worker replies must thread under it (the comment-triggered top-level guard).
// extraWorkerCols carry per-case worker-task overrides (e.g. autopilot_run_id)
// so negative guards reuse the same seed shape.
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

// TestDelegatedWorkerZeroRouteCommentWakesDelegator pins the core #9720 wiring:
// a completed delegated worker replies under the delegation thread without a
// mention, the comment routes to nobody, and the direct delegator still gets
// exactly one wake task triggered by that comment.
func TestDelegatedWorkerZeroRouteCommentWakesDelegator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 zero-route wake")
	seed := seedDelegationPair(t, issueID, "running", "completed", true)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)

	_, replyID := postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
		"closing report: done, no mention", seed.delegationComment)

	if got := tasksCarryingComment(t, issueID, seed.leaderAgent, replyID); got != 1 {
		t.Fatalf("leader tasks triggered by the zero-route reply = %d, want 1 (delegator not woken)", got)
	}
	if got := completionWakeCount(t, seed.workerTask); got != 1 {
		t.Fatalf("delegated_completion wake tasks = %d, want 1", got)
	}
}

// TestCoordinatorDirectMentionKeepsCurrentBehavior pins that an explicit
// @coordinator mention keeps its existing routing: the reply enqueues exactly
// one leader task through the mention path, and the zero-route wake never
// double-fires alongside it.
func TestCoordinatorDirectMentionKeepsCurrentBehavior(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 mention keeps behavior")
	// A terminal leader task keeps the mention on its plain fresh-enqueue path;
	// the worker is still running here (posting its result), which is the
	// mention route's ordinary current-behavior shape.
	seed := seedDelegationPair(t, issueID, "completed", "running", true)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)

	var leaderName string
	dbfx.QueryRow(t, `SELECT name FROM agent WHERE id = $1`, seed.leaderAgent).Scan(&leaderName)
	w, replyID := postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
		"done — [@"+leaderName+"](mention://agent/"+seed.leaderAgent+") please review", seed.delegationComment)

	var resp struct {
		TriggerOutcomes []CommentTriggerOutcome `json:"trigger_outcomes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode outcomes: %v", err)
	}
	found := false
	for _, o := range resp.TriggerOutcomes {
		if o.TargetType == "agent" && o.TargetID == seed.leaderAgent {
			found = true
			if o.Status != DispatchQueued {
				t.Fatalf("mention outcome for leader = %s, want queued (got %+v)", o.Status, resp.TriggerOutcomes)
			}
		}
	}
	if !found {
		t.Fatalf("no trigger outcome for leader agent in %+v", resp.TriggerOutcomes)
	}
	if got := tasksCarryingComment(t, issueID, seed.leaderAgent, replyID); got != 1 {
		t.Fatalf("leader tasks triggered by the mentioned reply = %d, want exactly 1", got)
	}
	if got := completionWakeCount(t, seed.workerTask); got != 0 {
		t.Fatalf("delegated_completion wake tasks = %d, want 0 (mention must not double-wake)", got)
	}
}

// TestWorkerTopLevelResultCommentWakesDelegator pins that the wake does not
// depend on thread parentage: when the worker task was not comment-triggered
// (no delegation comment exists) and posts its result top-level, the
// zero-route reply still wakes the delegator.
func TestWorkerTopLevelResultCommentWakesDelegator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 top-level result wake")
	seed := seedDelegationPair(t, issueID, "running", "completed", false)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)

	_, replyID := postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
		"result: shipped the delegated work", "")

	if got := tasksCarryingComment(t, issueID, seed.leaderAgent, replyID); got != 1 {
		t.Fatalf("leader tasks triggered by the top-level result = %d, want 1 (delegator not woken)", got)
	}
	if got := completionWakeCount(t, seed.workerTask); got != 1 {
		t.Fatalf("delegated_completion wake tasks = %d, want 1", got)
	}
}

// leaderTaskCountForIssue counts leader-agent task rows on the issue — the
// duplicate-detection probe for the pending case.
func leaderTaskCountForIssue(t *testing.T, issueID, leaderAgentID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issueID, leaderAgentID)
}

// TestExplicitMentionDoesNotDoubleWake pins the handler's zero-route guard: a
// completed delegated worker that mentions its coordinator routes through the
// mention path (non-empty triggers), so the wake must never fire beside it.
// The worker is seeded 'completed' deliberately: with the zero-route guard
// removed, loadDelegatedCompletionWakeTarget would otherwise return nil on
// worker status and the test would pass vacuously — 'completed' makes
// completionWakeCount == 0 genuinely pin the guard, and
// tasksCarryingComment == 1 pins that the mention route still enqueues
// exactly one leader task. A terminal authoring task resolves no human
// originator (invokeOriginatorFromRequest), which would make the private
// leader deny the mention outright, so the leader is seeded workspace-invoked
// (public_to + workspace target) — the agent-principal exception — purely so
// the mention route stays exercisable from a completed run.
func TestExplicitMentionDoesNotDoubleWake(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 mention no double wake")
	seed := seedDelegationPair(t, issueID, "completed", "completed", true)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)
	dbfx.Exec(t, `UPDATE agent SET permission_mode = 'public_to' WHERE id = $1`, seed.leaderAgent)
	dbfx.InsertNoID(t, "agent_invocation_target", testutil.Cols{
		"agent_id": seed.leaderAgent, "target_type": "workspace", "target_id": testWorkspaceID,
	}, `agent_id = $1 AND target_type = 'workspace'`, seed.leaderAgent)

	var leaderName string
	dbfx.QueryRow(t, `SELECT name FROM agent WHERE id = $1`, seed.leaderAgent).Scan(&leaderName)
	_, replyID := postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
		"done — [@"+leaderName+"](mention://agent/"+seed.leaderAgent+") please review", seed.delegationComment)

	if got := tasksCarryingComment(t, issueID, seed.leaderAgent, replyID); got != 1 {
		t.Fatalf("leader tasks triggered by the mentioned reply = %d, want exactly 1 (the mention route's; never two)", got)
	}
	if got := completionWakeCount(t, seed.workerTask); got != 0 {
		t.Fatalf("delegated_completion wake tasks = %d, want 0 (mention must not double-wake)", got)
	}
}

// TestAlreadyPendingDelegatorNotWokenAgain pins the fold-or-skip contract: a
// queued leader task already covers the wake, so the worker's zero-route
// closing comment must fold into it, never enqueue a second leader row.
// Non-vacuous: without the pending row the same comment enqueues a wake
// (positive tests), and the fold still records the comment receipt.
func TestAlreadyPendingDelegatorNotWokenAgain(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 pending delegator no second task")
	seed := seedDelegationPair(t, issueID, "running", "completed", true)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)
	// The leader's original delegator task is 'running' (dispatched); add a
	// queued leader row pending on the delegation thread (migration 516's
	// trigger derives comment_thread_id from trigger_comment_id) — the pending
	// slot the wake must merge into.
	dbfx.Task(t, seed.leaderAgent, testutil.Cols{
		"runtime_id": testRuntimeID, "issue_id": issueID, "status": "queued",
		"trigger_comment_id":  seed.delegationComment,
		"originator_user_id":  testUserID,
		"accountable_user_id": testUserID,
	})
	before := leaderTaskCountForIssue(t, issueID, seed.leaderAgent)
	if before != 2 {
		t.Fatalf("setup: leader tasks before comment = %d, want 2 (running + queued)", before)
	}

	_, replyID := postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
		"closing report: done, no mention", seed.delegationComment)

	if got := leaderTaskCountForIssue(t, issueID, seed.leaderAgent); got != before {
		t.Fatalf("leader tasks after comment = %d, want %d (fold-or-skip, never a second task)", got, before)
	}
	if got := tasksCarryingComment(t, issueID, seed.leaderAgent, replyID); got != 1 {
		t.Fatalf("leader tasks carrying the reply = %d, want 1 (the pending fold receipt)", got)
	}
	if got := completionWakeCount(t, seed.workerTask); got != 0 {
		t.Fatalf("delegated_completion wake tasks = %d, want 0 (pending task covers the wake)", got)
	}
}

// TestFailedWorkerTaskDoesNotWake pins that a failed delegated worker's
// zero-route comment does not take the completion wake: the failure side owns
// that handoff via the recovery outbox. Non-vacuous: the identical seed with
// worker status 'completed' (positive tests) produces exactly one wake, so the
// status check in loadDelegatedCompletionWakeTarget is the only gate being
// exercised here.
func TestFailedWorkerTaskDoesNotWake(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 failed worker no completion wake")
	seed := seedDelegationPair(t, issueID, "running", "failed", true)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)

	_, replyID := postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
		"failed mid-run, handing back", seed.delegationComment)

	if got := tasksCarryingComment(t, issueID, seed.leaderAgent, replyID); got != 0 {
		t.Fatalf("leader tasks triggered by the failed worker's reply = %d, want 0", got)
	}
	if got := completionWakeCount(t, seed.workerTask); got != 0 {
		t.Fatalf("delegated_completion wake tasks = %d, want 0", got)
	}
}

// TestArchivedDelegatorNotWoken pins the delegator-availability guard: an
// archived leader agent cannot receive the wake. Non-vacuous: archiving is the
// only difference from the positive seed (unarchived leader gets exactly one
// wake), so the ArchivedAt check in loadDelegatedCompletionWakeTarget is what
// this exercises.
func TestArchivedDelegatorNotWoken(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 archived delegator no wake")
	seed := seedDelegationPair(t, issueID, "running", "completed", true)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)
	dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, seed.leaderAgent)

	_, replyID := postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
		"closing report: done, no mention", seed.delegationComment)

	if got := tasksCarryingComment(t, issueID, seed.leaderAgent, replyID); got != 0 {
		t.Fatalf("leader tasks triggered by the reply = %d, want 0 (archived delegator)", got)
	}
	if got := completionWakeCount(t, seed.workerTask); got != 0 {
		t.Fatalf("delegated_completion wake tasks = %d, want 0", got)
	}
}

// TestDelegatorWithoutRuntimeNotWoken pins the runtime guard: a leader agent
// with no runtime cannot execute a wake task. Non-vacuous: nulling runtime_id
// is the only difference from the positive seed, so the RuntimeID check in
// loadDelegatedCompletionWakeTarget is what this exercises.
func TestDelegatorWithoutRuntimeNotWoken(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 runtime-less delegator no wake")
	seed := seedDelegationPair(t, issueID, "running", "completed", true)
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)
	dbfx.Exec(t, `UPDATE agent SET runtime_id = NULL WHERE id = $1`, seed.leaderAgent)

	_, replyID := postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
		"closing report: done, no mention", seed.delegationComment)

	if got := tasksCarryingComment(t, issueID, seed.leaderAgent, replyID); got != 0 {
		t.Fatalf("leader tasks triggered by the reply = %d, want 0 (delegator without runtime)", got)
	}
	if got := completionWakeCount(t, seed.workerTask); got != 0 {
		t.Fatalf("delegated_completion wake tasks = %d, want 0", got)
	}
}

// TestAutopilotDelegatedRunDoesNotWake pins the autopilot guard: when an
// autopilot run owns the delegated worker task, its closing comment must not
// spawn a coordinator wake (autopilot owns the follow-up). agent_task_queue.
// autopilot_run_id still carries a legacy FK to autopilot_run (migration 042
// predates the no-FK rule), so a real autopilot + autopilot_run pair is
// seeded. Non-vacuous: the autopilot_run_id column is the only difference
// from the positive seed, so the AutopilotRunID check in
// loadDelegatedCompletionWakeTarget is what this exercises.
func TestAutopilotDelegatedRunDoesNotWake(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := seedWakeIssue(t, "9720 autopilot run no wake")
	// autopilot.assignee_id references agent(id); any live agent satisfies it.
	autopilotOwner := dbfx.Agent(t, "9720 autopilot owner", testRuntimeID)
	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id": testWorkspaceID, "title": "9720 wake autopilot",
		"assignee_id": autopilotOwner, "created_by_type": "member", "created_by_id": testUserID,
	})
	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": autopilotID, "source": "manual", "status": "running",
	})
	seed := seedDelegationPair(t, issueID, "running", "completed", true, testutil.Cols{
		"autopilot_run_id": runID,
	})
	dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, seed.leaderAgent)

	_, replyID := postCommentAsAgent(t, issueID, seed.workerAgent, seed.workerTask,
		"closing report: done, no mention", seed.delegationComment)

	if got := tasksCarryingComment(t, issueID, seed.leaderAgent, replyID); got != 0 {
		t.Fatalf("leader tasks triggered by the reply = %d, want 0 (autopilot-owned run)", got)
	}
	if got := completionWakeCount(t, seed.workerTask); got != 0 {
		t.Fatalf("delegated_completion wake tasks = %d, want 0", got)
	}
}
