package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type leaderMentionFixture struct {
	runtime, leader, worker, squad, issue, leaderTask, parent, workerTask string
	tokenUser                                                             string
}

func newLeaderMentionFixture(t *testing.T) leaderMentionFixture {
	t.Helper()
	f := leaderMentionFixture{}
	f.runtime = dbfx.Runtime(t, "Mention coordinator")
	f.leader = dbfx.Agent(t, "Coordinator", f.runtime)
	f.worker = dbfx.Agent(t, "Reporter", dbfx.Runtime(t, "Mention reporter"))
	f.squad = dbfx.Squad(t, "Review squad", f.leader)
	f.issue = dbfx.Issue(t, "Verify report routing", testutil.Cols{"status": "in_progress", "assignee_type": "squad", "assignee_id": f.squad})
	f.leaderTask = dbfx.Task(t, f.leader, testutil.Cols{"runtime_id": f.runtime, "issue_id": f.issue, "status": "completed", "is_leader_task": true, "squad_id": f.squad, "originator_user_id": testUserID, "accountable_user_id": testUserID})
	f.parent = dbfx.Comment(t, f.issue, mention(f.worker)+" verify this change", testutil.Cols{"author_type": "agent", "author_id": f.leader, "source_task_id": f.leaderTask})
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(f.issue))
	if err != nil {
		t.Fatal(err)
	}
	workerTask, err := testHandler.TaskService.EnqueueTaskForMention(context.Background(), issue, parseUUID(f.worker), parseUUID(f.parent), service.OriginNamed)
	if err != nil {
		t.Fatal(err)
	}
	f.workerTask = uuidToString(workerTask.ID)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status='running' WHERE id=$1`, f.workerTask)
	f.tokenUser = testUserID
	// Runs/comments created by the handler are not fixture-owned inserts.
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE issue_id=$1`, f.issue)
	dbfx.Cleanup(t, `DELETE FROM comment WHERE issue_id=$1`, f.issue)
	return f
}

func (f leaderMentionFixture) report(t *testing.T, content string, steer ...bool) CommentResponse {
	t.Helper()
	token := "mat_synthetic_leader_report_" + f.workerTask
	dbfx.Insert(t, "task_token", testutil.Cols{"token_hash": auth.HashToken(token), "task_id": f.workerTask, "agent_id": f.worker, "workspace_id": testWorkspaceID, "user_id": f.tokenUser, "expires_at": time.Now().Add(time.Hour)})
	body := map[string]any{"content": content, "parent_id": f.parent}
	if len(steer) > 0 {
		body["steer"] = steer[0]
	}
	req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+f.issue+"/comments", body), "id", f.issue)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Agent-ID", "forged-agent")
	req.Header.Set("X-Task-ID", "forged-task")
	protected := middleware.Auth(testHandler.Queries, nil, nil, nil)(middleware.RequireWorkspaceMember(testHandler.Queries)(http.HandlerFunc(testHandler.CreateComment)))
	var response CommentResponse
	testutil.Call(t, protected.ServeHTTP, req).Want(http.StatusCreated).JSON(&response)
	if response.AuthorID != f.worker || response.SourceTaskID == nil || *response.SourceTaskID != f.workerTask {
		t.Fatalf("report author/source were not token-bound: %+v", response)
	}
	return response
}

func TestWorkerLeaderMention_ClaimAndEvaluation(t *testing.T) {
	for _, mode := range []string{"fresh", "pending", "pending agent then squad", "pending squad then agent"} {
		t.Run(mode, func(t *testing.T) {
			pending := mode != "fresh"
			f := newLeaderMentionFixture(t)
			f.tokenUser = dbfx.User(t, "Runtime principal", "runtime-"+f.workerTask+"@example.test")
			dbfx.Member(t, testWorkspaceID, f.tokenUser, "member")
			var pendingID string
			if pending {
				pendingID = dbfx.Task(t, f.leader, testutil.Cols{"runtime_id": f.runtime, "issue_id": f.issue, "status": "queued", "trigger_comment_id": f.parent, "originator_user_id": testUserID, "accountable_user_id": testUserID})
			}
			content := mention(f.leader) + " verification complete"
			squadMention := "[@Review squad](mention://squad/" + f.squad + ")"
			if mode == "pending agent then squad" {
				content += " " + squadMention
			}
			if mode == "pending squad then agent" {
				content = squadMention + " " + content
			}
			report := f.report(t, content)
			var taskID string
			dbfx.QueryRow(t, `SELECT id FROM agent_task_queue WHERE issue_id=$1 AND agent_id=$2 AND status='queued'`, f.issue, f.leader).Scan(&taskID)
			task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			if !task.IsLeaderTask || task.SquadID != parseUUID(f.squad) {
				t.Fatalf("worker report was demoted to a personal run: leader=%t squad=%s", task.IsLeaderTask, uuidToString(task.SquadID))
			}
			if pending && taskID != pendingID {
				t.Fatal("report did not coalesce into the pending run")
			}
			if task.OriginatorUserID != parseUUID(testUserID) || task.AccountableUserID != parseUUID(testUserID) || task.DelegatedFromTaskID != parseUUID(f.workerTask) {
				t.Fatal("report lost its authorizing human or delegation lineage")
			}
			claimed := claimWorkerReplyRun(t, f.runtime)
			if claimed == nil || claimed.ID != taskID || !claimed.IsLeaderTask || claimed.Agent == nil || !strings.Contains(claimed.Agent.Instructions, "squad activity") {
				t.Fatal("claim lost the coordinator role or briefing")
			}
			if claimed.Attribution == nil || claimed.Attribution.Originator == nil || claimed.Attribution.Originator.ID != testUserID {
				t.Fatal("claim replaced the authorizing human with the runtime principal")
			}
			dbfx.Exec(t, `UPDATE agent_task_queue SET dispatched_at=now()-interval '5 minutes',prepare_lease_expires_at=NULL WHERE id=$1`, taskID)
			reclaimed := claimWorkerReplyRun(t, f.runtime)
			if reclaimed == nil || reclaimed.ID != taskID || !reclaimed.IsLeaderTask {
				t.Fatal("reclaim lost the leader role")
			}
			testutil.Call(t, testHandler.RecordSquadLeaderEvaluation, evaluationRequest(f.issue, f.leader, taskID, "no_action")).Want(http.StatusCreated)
			evaluations := loadEvaluations(t, f.issue)
			if len(evaluations) != 1 || evaluations[0].SquadID != f.squad || evaluations[0].ActorID != f.leader {
				t.Fatalf("incorrect evaluation audit: %+v", evaluations)
			}
			if task.TriggerCommentID != parseUUID(report.ID) {
				t.Fatal("report was not the leader run's trigger")
			}
		})
	}
}

func TestWorkerLeaderMention_ProvenanceBoundaries(t *testing.T) {
	for _, mode := range []string{"valid", "member", "no parent", "no worker task", "authoring task is leader", "uncovered parent", "worker task from another issue", "worker identity mismatch", "foreign worker", "leader identity mismatch", "leader task from another issue", "leader task is worker", "missing source task", "deleted delegation", "changed leader", "archived squad", "different assigned squad", "foreign squad", "dual role self mention", "coalesced delegation", "newer unrelated worker run", "invocation denied"} {
		t.Run(mode, func(t *testing.T) {
			f := newLeaderMentionFixture(t)
			ctx := context.Background()
			authorType, authorID := "agent", f.worker
			authoring := parseUUID(f.workerTask)
			switch mode {
			case "member":
				authorType, authorID = "member", testUserID
			case "authoring task is leader":
				dbfx.Exec(t, `UPDATE agent_task_queue SET is_leader_task=true WHERE id=$1`, f.workerTask)
			case "no worker task":
				authoring = pgtype.UUID{}
			case "uncovered parent":
				dbfx.Exec(t, `UPDATE agent_task_queue SET trigger_comment_id=NULL WHERE id=$1`, f.workerTask)
			case "worker task from another issue":
				dbfx.Exec(t, `UPDATE agent_task_queue SET issue_id=$2 WHERE id=$1`, f.workerTask, dbfx.Issue(t, "Unrelated work"))
			case "foreign worker":
				ws := dbfx.Insert(t, "workspace", testutil.Cols{"name": "Foreign workspace", "slug": "foreign-" + f.worker})
				dbfx.Exec(t, `UPDATE agent SET workspace_id=$2 WHERE id=$1`, f.worker, ws)
			case "leader identity mismatch":
				dbfx.Exec(t, `UPDATE agent_task_queue SET agent_id=$2 WHERE id=$1`, f.leaderTask, f.worker)
			case "worker identity mismatch":
				authorID = f.leader
			case "leader task from another issue":
				dbfx.Exec(t, `UPDATE agent_task_queue SET issue_id=$2 WHERE id=$1`, f.leaderTask, dbfx.Issue(t, "Other coordination"))
			case "leader task is worker":
				dbfx.Exec(t, `UPDATE agent_task_queue SET is_leader_task=false WHERE id=$1`, f.leaderTask)
			case "missing source task":
				dbfx.Exec(t, `UPDATE comment SET source_task_id=NULL WHERE id=$1`, f.parent)
			case "deleted delegation":
				dbfx.Exec(t, `UPDATE comment SET deleted_at=now() WHERE id=$1`, f.parent)
			case "changed leader":
				dbfx.Exec(t, `UPDATE squad SET leader_id=$2 WHERE id=$1`, f.squad, f.worker)
			case "archived squad":
				dbfx.Exec(t, `UPDATE squad SET archived_at=now() WHERE id=$1`, f.squad)
			case "different assigned squad":
				dbfx.Exec(t, `UPDATE issue SET assignee_id=$2 WHERE id=$1`, f.issue, dbfx.Squad(t, "Other squad", f.leader))
			case "foreign squad":
				ws := dbfx.Insert(t, "workspace", testutil.Cols{"name": "Other workspace", "slug": "other-" + f.squad})
				dbfx.Exec(t, `UPDATE squad SET workspace_id=$2 WHERE id=$1`, f.squad, ws)
			case "dual role self mention":
				authorID = f.leader
				dbfx.Exec(t, `UPDATE agent_task_queue SET agent_id=$2 WHERE id=$1`, f.workerTask, f.leader)
			case "coalesced delegation":
				dbfx.Exec(t, `UPDATE agent_task_queue SET trigger_comment_id=NULL,coalesced_comment_ids=ARRAY[$2::uuid] WHERE id=$1`, f.workerTask, f.parent)
			case "newer unrelated worker run":
				dbfx.Task(t, f.worker, testutil.Cols{"runtime_id": f.runtime, "issue_id": f.issue, "status": "completed", "is_leader_task": true, "squad_id": f.squad})
			case "invocation denied":
				dbfx.Exec(t, `UPDATE agent_task_queue SET originator_user_id=NULL,accountable_user_id=NULL WHERE id=$1`, f.workerTask)
			}
			issue, err := testHandler.Queries.GetIssue(ctx, parseUUID(f.issue))
			if err != nil {
				t.Fatal(err)
			}
			parent, err := testHandler.Queries.GetComment(ctx, parseUUID(f.parent))
			if err != nil {
				t.Fatal(err)
			}
			originator := testUserID
			if mode == "invocation denied" {
				originator = ""
			}
			parentPtr := &parent
			if mode == "no parent" {
				parentPtr = nil
			}
			triggers, _ := testHandler.computeCommentAgentTriggers(ctx, issue, mention(f.leader), parentPtr, authorType, authorID, commentTriggerComputeOptions{AuthoringTaskID: authoring, OriginatorUserID: originator})
			want := mode == "valid" || mode == "coalesced delegation" || mode == "newer unrelated worker run"
			got := len(triggers) == 1 && triggers[0].Squad != nil && triggers[0].Squad.ID == parseUUID(f.squad)
			if mode == "invocation denied" && len(triggers) != 0 {
				t.Fatal("report bypassed the leader invocation gate")
			}
			if got != want {
				t.Fatalf("leader role=%t, want %t (triggers=%+v)", got, want, triggers)
			}
		})
	}
}

func TestWorkerLeaderMention_ClaimedPersonalRunFollowup(t *testing.T) {
	f := newLeaderMentionFixture(t)
	pendingID := dbfx.Task(t, f.leader, testutil.Cols{"runtime_id": f.runtime, "issue_id": f.issue, "status": "queued", "trigger_comment_id": f.parent, "originator_user_id": testUserID, "accountable_user_id": testUserID})
	first := claimWorkerReplyRun(t, f.runtime)
	if first == nil || first.ID != pendingID || first.IsLeaderTask {
		t.Fatal("expected an already-claimed personal run")
	}
	report := f.report(t, mention(f.leader)+" please evaluate these results", true)
	old, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(pendingID))
	if err != nil {
		t.Fatal(err)
	}
	if old.IsLeaderTask {
		t.Fatal("changed the role after its claim was delivered")
	}
	if _, err := testHandler.TaskService.StartTask(context.Background(), parseUUID(pendingID)); err != nil {
		t.Fatal(err)
	}
	completeWorkerReplyRun(t, pendingID)
	next := claimWorkerReplyRun(t, f.runtime)
	if next == nil || !next.IsLeaderTask || next.Agent == nil || !strings.Contains(next.Agent.Instructions, "squad activity") {
		t.Fatal("report did not produce a coordinator follow-up")
	}
	if next.TriggerCommentID == nil || *next.TriggerCommentID != report.ID {
		t.Fatal("follow-up lost the report")
	}
}

func TestWorkerLeaderMention_PendingOtherSquadIsPreserved(t *testing.T) {
	f := newLeaderMentionFixture(t)
	other := dbfx.Squad(t, "Other coordinator role", f.leader)
	pending := dbfx.Task(t, f.leader, testutil.Cols{"runtime_id": f.runtime, "issue_id": f.issue, "status": "queued", "trigger_comment_id": f.parent, "squad_id": other, "is_leader_task": true, "originator_user_id": testUserID, "accountable_user_id": testUserID})
	f.report(t, mention(f.leader)+" result for the assigned squad")
	stored, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(pending))
	if err != nil {
		t.Fatal(err)
	}
	if !stored.IsLeaderTask || stored.SquadID != parseUUID(other) || stored.TriggerCommentID != parseUUID(f.parent) {
		t.Fatal("report overwrote another squad's pending role")
	}
}

func TestWorkerLeaderMention_MergeLosesClaimRace(t *testing.T) {
	f := newLeaderMentionFixture(t)
	pending := dbfx.Task(t, f.leader, testutil.Cols{"runtime_id": f.runtime, "issue_id": f.issue, "status": "queued", "trigger_comment_id": f.parent})
	report := dbfx.Comment(t, f.issue, mention(f.leader)+" verified", testutil.Cols{"parent_id": f.parent, "author_type": "agent", "author_id": f.worker, "source_task_id": f.workerTask})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, `UPDATE agent_task_queue SET status='dispatched' WHERE id=$1`, pending); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := db.New(conn).MergeCommentIntoPendingTask(ctx, db.MergeCommentIntoPendingTaskParams{
			IssueID: parseUUID(f.issue), AgentID: parseUUID(f.leader), NewTriggerCommentID: parseUUID(report), WorkerReportSquadID: parseUUID(f.squad),
			NewOriginatorUserID: parseUUID(testUserID), NewAccountableUserID: parseUUID(testUserID),
		})
		done <- err
	}()
	// The merge sees the old queued snapshot and waits on the claim's row lock.
	var waiting bool
	for !waiting && ctx.Err() == nil {
		if err := testPool.QueryRow(ctx, `SELECT COALESCE(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1`, conn.Conn().PgConn().PID()).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if !waiting {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !waiting {
		t.Fatal("merge did not wait for the concurrent claim")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("merge after claim: got %v, want no queued candidate", err)
	}
	stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(pending))
	if err != nil {
		t.Fatal(err)
	}
	if stored.IsLeaderTask || stored.SquadID.Valid || stored.TriggerCommentID != parseUUID(f.parent) {
		t.Fatal("merge rewrote an already-dispatched personal run")
	}
}
