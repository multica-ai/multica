package handler

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type workerCompletionFixture struct {
	issueID, squadID, leaderID, leaderRuntimeID string
	workerID, workerTaskID, delegationID        string
}

func postCompletionFixtureComment(t *testing.T, issueID, parentID, agentID, taskID, content string) CommentResponse {
	t.Helper()
	body := map[string]any{"content": content}
	if parentID != "" {
		body["parent_id"] = parentID
	}
	req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", body), "id", issueID)
	if agentID != "" {
		req.Header.Set("X-Agent-ID", agentID)
		req.Header.Set("X-Task-ID", taskID)
	}
	var response CommentResponse
	testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusCreated).JSON(&response)
	return response
}

func reportCompletionOutput(t *testing.T, taskID, output string) {
	t.Helper()
	req := withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": output}, testWorkspaceID, "worker-completion-handoff"), "taskId", taskID)
	testutil.Call(t, testHandler.CompleteTask, req).Want(http.StatusOK)
}

// Delegate through the real comment endpoint, then claim/start the worker so
// its originator, source task and delivered inputs come from production paths.
func newWorkerCompletionFixture(t *testing.T) workerCompletionFixture {
	t.Helper()
	ctx := context.Background()
	fx := workerCompletionFixture{}
	fx.leaderRuntimeID = dbfx.Runtime(t, "Completion handoff leader runtime")
	fx.leaderID = dbfx.Agent(t, "Completion handoff leader", fx.leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
	workerRuntimeID := dbfx.Runtime(t, "Completion handoff worker runtime")
	fx.workerID = dbfx.Agent(t, "Completion handoff worker", workerRuntimeID)
	fx.squadID = dbfx.Squad(t, "Completion handoff squad", fx.leaderID)
	dbfx.SquadMember(t, fx.squadID, "agent", fx.workerID)
	fx.issueID = dbfx.Issue(t, "Worker completion result must reach its leader", testutil.Cols{
		"status": "in_progress", "assignee_type": "squad", "assignee_id": fx.squadID,
	})
	sourceID := dbfx.Task(t, fx.leaderID, testutil.Cols{
		"runtime_id": fx.leaderRuntimeID, "issue_id": fx.issueID, "status": "running",
		"started_at": testutil.Raw("now()"), "is_leader_task": true, "squad_id": fx.squadID,
		"originator_user_id": testUserID, "accountable_user_id": testUserID,
	})
	// Handler-created rows are not registered by individual fixture inserts.
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, fx.issueID)
	dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id=$1`, fx.issueID)
	delegation := postCompletionFixtureComment(t, fx.issueID, "", fx.leaderID, sourceID,
		fmt.Sprintf("[@Worker](mention://agent/%s) verify the implementation", fx.workerID))
	fx.delegationID = delegation.ID
	worker := claimWorkerReplyRun(t, workerRuntimeID)
	if worker == nil || worker.TriggerCommentID == nil || *worker.TriggerCommentID != delegation.ID {
		t.Fatal("the worker did not claim its leader's delegation")
	}
	fx.workerTaskID = worker.ID
	if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(worker.ID)); err != nil {
		t.Fatal(err)
	}
	completeWorkerReplyRun(t, sourceID)
	return fx
}

func TestWorkerCompletionCommentHandoff(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, state := range []string{"idle", "queued", "dispatched", "running"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			fx := newWorkerCompletionFixture(t)
			var priorID, holderID string
			var first *AgentTaskResponse
			if state != "idle" {
				prior := postCompletionFixtureComment(t, fx.issueID, fx.delegationID, "", "", "Please coordinate the incoming worker result")
				priorID = prior.ID
				dbfx.QueryRow(t, `SELECT id FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND status='queued'`, fx.issueID, fx.leaderID).Scan(&holderID)
				if state != "queued" {
					first = claimWorkerReplyRun(t, fx.leaderRuntimeID)
					if first == nil || first.ID != holderID || !slices.Contains(first.DeliveredCommentIDs, priorID) {
						t.Fatal("the leader did not receive the earlier request")
					}
				}
				if state == "running" {
					if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(holderID)); err != nil {
						t.Fatal(err)
					}
				}
			}
			const output = "Verification complete: the retry duplicates the database write; please coordinate a repair"
			reportCompletionOutput(t, fx.workerTaskID, output)
			var commentID string
			dbfx.QueryRow(t, `SELECT id FROM comment WHERE issue_id=$1 AND source_task_id=$2 AND content=$3 AND type='comment'`, fx.issueID, fx.workerTaskID, output).Scan(&commentID)
			// Repeated completion must not create another result or dispatch it twice.
			reportCompletionOutput(t, fx.workerTaskID, output)
			if state == "dispatched" {
				stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(holderID))
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Contains(stored.CoalescedCommentIds, parseUUID(commentID)) || slices.Contains(stored.DeliveredCommentIds, parseUUID(commentID)) {
					t.Fatal("a claimed leader must record the worker result as planned, not delivered")
				}
				if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(holderID)); err != nil {
					t.Fatal(err)
				}
			}
			if state == "dispatched" || state == "running" {
				if another := claimWorkerReplyRun(t, fx.leaderRuntimeID); another != nil {
					t.Fatal("leader runs for the same issue must remain serialized")
				}
				completeWorkerReplyRun(t, holderID)
			}
			next := claimWorkerReplyRun(t, fx.leaderRuntimeID)
			if next == nil {
				t.Fatal("the synthesized worker result was saved but never delivered to the leader")
			}
			if next.TriggerCommentContent != output || !slices.Contains(next.DeliveredCommentIDs, commentID) || !next.IsLeaderTask {
				t.Fatal("the leader must receive the exact result, receipt and squad role")
			}
			if state == "queued" && (next.ID != holderID || !slices.Contains(next.DeliveredCommentIDs, priorID)) {
				t.Fatal("a queued leader must coalesce both inputs into the existing run")
			}
			stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(next.ID))
			if err != nil {
				t.Fatal(err)
			}
			if stored.SquadID != parseUUID(fx.squadID) || stored.OriginatorUserID != parseUUID(testUserID) || stored.AccountableUserID != parseUUID(testUserID) || stored.DelegatedFromTaskID != parseUUID(fx.workerTaskID) {
				t.Fatal("the result handoff lost its squad or human delegation lineage")
			}
			if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(next.ID)); err != nil {
				t.Fatal(err)
			}
			completeWorkerReplyRun(t, next.ID)
			reportCompletionOutput(t, fx.workerTaskID, output)
			if extra := claimWorkerReplyRun(t, fx.leaderRuntimeID); extra != nil {
				t.Fatal("a completed handoff or the leader's own fallback result must not restart the leader")
			}
			var comments int
			dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE source_task_id=$1`, fx.workerTaskID).Scan(&comments)
			if comments != 1 {
				t.Fatalf("worker result comments = %d, want 1", comments)
			}
		})
	}
}

func TestWorkerCompletionCommentRoutingBoundaries(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, mode := range []string{"note", "member_mention", "broadcast", "agent_mention", "squad_mention", "archived_leader", "archived_squad", "private_leader", "member_assignee", "guest_delegation", "changed_leader", "unattributed_delegation", "member_delegation", "blank", "trivial", "already_posted"} {
		t.Run(mode, func(t *testing.T) {
			fx := newWorkerCompletionFixture(t)
			output := "Verification finished; the leader should coordinate the repair"
			wantComments := 1
			switch mode {
			case "note":
				output = "/note " + output
			case "member_mention":
				output = fmt.Sprintf("[@Human](mention://member/%s) %s", testUserID, output)
			case "broadcast":
				output = "[@all](mention://all/all) " + output
			case "agent_mention":
				output = fmt.Sprintf("[@Leader](mention://agent/%s) %s", fx.leaderID, output)
			case "squad_mention":
				output = fmt.Sprintf("[@Squad](mention://squad/%s) %s", fx.squadID, output)
			case "archived_leader":
				dbfx.Exec(t, `UPDATE agent SET archived_at=now() WHERE id=$1`, fx.leaderID)
			case "archived_squad":
				dbfx.Exec(t, `UPDATE squad SET archived_at=now() WHERE id=$1`, fx.squadID)
			case "private_leader":
				owner := dbfx.User(t, "Private leader owner", "worker-completion-private-"+fx.leaderID+"@example.invalid")
				dbfx.Exec(t, `UPDATE agent SET owner_id=$2, permission_mode='private' WHERE id=$1`, fx.leaderID, owner)
				dbfx.Cleanup(t, `UPDATE agent SET owner_id=$2 WHERE id=$1`, fx.leaderID, testUserID)
			case "member_assignee":
				dbfx.Exec(t, `UPDATE issue SET assignee_type='member', assignee_id=$2 WHERE id=$1`, fx.issueID, testUserID)
			case "guest_delegation":
				otherLeader := dbfx.Agent(t, "Other assigned leader", fx.leaderRuntimeID)
				otherSquad := dbfx.Squad(t, "Other assigned squad", otherLeader)
				dbfx.Exec(t, `UPDATE issue SET assignee_id=$2 WHERE id=$1`, fx.issueID, otherSquad)
				dbfx.Cleanup(t, `UPDATE issue SET assignee_id=$2 WHERE id=$1`, fx.issueID, fx.squadID)
			case "changed_leader":
				otherLeader := dbfx.Agent(t, "Replacement leader", fx.leaderRuntimeID)
				dbfx.Exec(t, `UPDATE squad SET leader_id=$2 WHERE id=$1`, fx.squadID, otherLeader)
				dbfx.Cleanup(t, `UPDATE squad SET leader_id=$2 WHERE id=$1`, fx.squadID, fx.leaderID)
			case "unattributed_delegation":
				dbfx.Exec(t, `UPDATE comment SET source_task_id=NULL WHERE id=$1`, fx.delegationID)
			case "member_delegation":
				dbfx.Exec(t, `UPDATE comment SET author_type='member', author_id=$2, source_task_id=NULL WHERE id=$1`, fx.delegationID, testUserID)
			case "blank":
				output, wantComments = "", 0
			case "trivial":
				output, wantComments = "Done.", 0
			case "already_posted":
				posted := postCompletionFixtureComment(t, fx.issueID, fx.delegationID, fx.workerID, fx.workerTaskID, output)
				leader := claimWorkerReplyRun(t, fx.leaderRuntimeID)
				if leader == nil || !slices.Contains(leader.DeliveredCommentIDs, posted.ID) {
					t.Fatal("the explicitly posted result did not reach the leader")
				}
				if _, err := testHandler.TaskService.StartTask(context.Background(), parseUUID(leader.ID)); err != nil {
					t.Fatal(err)
				}
				completeWorkerReplyRun(t, leader.ID)
			}
			reportCompletionOutput(t, fx.workerTaskID, output)
			if leader := claimWorkerReplyRun(t, fx.leaderRuntimeID); leader != nil {
				t.Fatal("completion must preserve the routing/suppression boundary")
			}
			var pending int
			dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND status IN ('queued','dispatched','running')`, fx.issueID).Scan(&pending)
			if pending != 0 {
				t.Fatalf("unexpected pending runs on another target: %d", pending)
			}
			var comments int
			dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE source_task_id=$1 AND type='comment'`, fx.workerTaskID).Scan(&comments)
			if comments != wantComments {
				t.Fatalf("result comments = %d, want %d", comments, wantComments)
			}
		})
	}
}

func TestWorkerCompletionCommentConcurrentCallbacks(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newWorkerCompletionFixture(t)
	ctx := context.Background()
	const output = "Verification complete: the leader can now schedule the repair"
	var calls atomic.Int32
	previous := testHandler.TaskService.OnCompletionComment
	testHandler.TaskService.OnCompletionComment = func(ctx context.Context, issue db.Issue, comment db.Comment) {
		calls.Add(1)
		previous(ctx, issue, comment)
	}
	t.Cleanup(func() { testHandler.TaskService.OnCompletionComment = previous })
	start := make(chan struct{})
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			<-start
			_, err := testHandler.TaskService.CompleteTask(ctx, parseUUID(fx.workerTaskID),
				[]byte(`{"output":"Verification complete: the leader can now schedule the repair"}`), "", "", "", false, "", "")
			errors <- err
		})
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("completion handoff callbacks = %d, want 1", calls.Load())
	}
	var comments, pending int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE source_task_id=$1 AND content=$2`, fx.workerTaskID, output).Scan(&comments)
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND status='queued'`, fx.issueID, fx.leaderID).Scan(&pending)
	if comments != 1 || pending != 1 {
		t.Fatalf("result comments=%d leader runs=%d, want 1 each", comments, pending)
	}
	leader := claimWorkerReplyRun(t, fx.leaderRuntimeID)
	if leader == nil || leader.TriggerCommentContent != output || len(leader.DeliveredCommentIDs) != 1 {
		t.Fatal("the sole handoff did not deliver its result")
	}
}

func TestWorkerFailureCommentDoesNotInvokeCompletionHandoff(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	fx := newWorkerCompletionFixture(t)
	calls := 0
	previous := testHandler.TaskService.OnCompletionComment
	testHandler.TaskService.OnCompletionComment = func(ctx context.Context, issue db.Issue, comment db.Comment) {
		calls++
		previous(ctx, issue, comment)
	}
	t.Cleanup(func() { testHandler.TaskService.OnCompletionComment = previous })
	req := withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+fx.workerTaskID+"/fail",
		map[string]any{"error": "Verification could not complete", "failure_reason": "agent_error"}, testWorkspaceID, "worker-completion-handoff"), "taskId", fx.workerTaskID)
	testutil.Call(t, testHandler.FailTask, req).Want(http.StatusOK)
	if calls != 0 {
		t.Fatal("failure diagnostics must not enter the success-comment handoff")
	}
	var diagnostics int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE source_task_id=$1 AND type='system'`, fx.workerTaskID).Scan(&diagnostics)
	if diagnostics != 1 {
		t.Fatalf("failure diagnostics = %d, want 1", diagnostics)
	}
}
