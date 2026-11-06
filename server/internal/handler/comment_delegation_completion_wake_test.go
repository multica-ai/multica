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
func seedDelegationPair(t *testing.T, issueID, leaderStatus, workerStatus string, withDelegationComment bool) delegatedWakeSeed {
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
