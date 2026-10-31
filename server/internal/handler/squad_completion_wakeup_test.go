package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestSquadNoActionWorkerCompletion(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, order := range []string{"after_leader", "individually_assigned", "before_evaluation", "while_leader", "ordinary_queued", "leader_queued", "merged_member", "merged_workers", "before_claim", "completion_commit_after_claim", "disabled", "deleted", "rotated", "closed", "failed", "cancelled", "contention", "wrong_source", "historical", "capacity", "reenabled"} {
		t.Run(order, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := dbfx.Runtime(t, "Completion leader runtime")
			leaderID := dbfx.Agent(t, "Completion leader", runtimeID)
			workerRuntime := dbfx.Runtime(t, "Completion worker runtime")
			workerID := dbfx.Agent(t, "Completion worker", workerRuntime)
			squadID := dbfx.Squad(t, "Completion squad", leaderID)
			dbfx.SquadMember(t, squadID, "agent", workerID)
			issueID := dbfx.Issue(t, "Coordinate remaining work", testutil.Cols{"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID})
			if order == "individually_assigned" {
				dbfx.Exec(t, "UPDATE issue SET assignee_type='agent',assignee_id=$2 WHERE id=$1", issueID, leaderID)
			}
			sourceID := dbfx.Task(t, leaderID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "completed", "is_leader_task": true, "squad_id": squadID, "originator_user_id": testUserID, "accountable_user_id": testUserID})
			rootID := dbfx.Comment(t, issueID, "Review the implementation", testutil.Cols{"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID})
			workerTaskID := dbfx.Task(t, workerID, testutil.Cols{"runtime_id": workerRuntime, "issue_id": issueID, "status": "running", "started_at": testutil.Raw("now()"), "trigger_comment_id": rootID, "delegated_from_task_id": sourceID, "squad_id": squadID, "originator_user_id": testUserID, "accountable_user_id": testUserID})
			req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": "Review finished; a repair is still needed", "parent_id": rootID}), "id", issueID)
			req.Header.Set("X-Agent-ID", workerID)
			req.Header.Set("X-Task-ID", workerTaskID)
			testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusCreated)
			var secondWorker string
			if order == "merged_workers" {
				secondID := dbfx.Agent(t, "Second completion worker", workerRuntime)
				dbfx.SquadMember(t, squadID, "agent", secondID)
				secondWorker = dbfx.Task(t, secondID, testutil.Cols{"runtime_id": workerRuntime, "issue_id": issueID, "status": "running", "started_at": testutil.Raw("now()"), "trigger_comment_id": rootID, "delegated_from_task_id": sourceID, "squad_id": squadID, "originator_user_id": testUserID, "accountable_user_id": testUserID})
				req = withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": "Second review result", "parent_id": rootID}), "id", issueID)
				req.Header.Set("X-Agent-ID", secondID)
				req.Header.Set("X-Task-ID", secondWorker)
				testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusCreated)
			}
			if order == "merged_member" {
				req = withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{"content": "Keep the existing restrictions", "parent_id": rootID}), "id", issueID)
				testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusCreated)
			}
			if order == "before_claim" {
				completeWorkerReplyRun(t, workerTaskID)
			}
			// now() is the completion transaction's start, not its commit time.
			var finishCompletion func()
			if order == "completion_commit_after_claim" {
				tx, err := testPool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(ctx)
				if _, err = tx.Exec(ctx, "SELECT now()"); err != nil {
					t.Fatal(err)
				}
				finishCompletion = func() {
					if _, err := tx.Exec(ctx, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1", workerTaskID); err != nil {
						t.Fatal(err)
					}
					if err := tx.Commit(ctx); err != nil {
						t.Fatal(err)
					}
				}
			}
			first := claimSquadCompletionRun(t, runtimeID)
			if first == nil || !first.IsLeaderTask {
				t.Fatal("worker result did not wake the leader")
			}
			if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(first.ID)); err != nil {
				t.Fatal(err)
			}
			if finishCompletion != nil {
				finishCompletion()
			}
			fx := runningSquadLeaderTaskFixture{IssueID: issueID, SquadID: squadID, LeaderID: leaderID, TaskID: first.ID}
			if order == "before_evaluation" {
				completeWorkerReplyRun(t, workerTaskID)
			}
			if order == "wrong_source" {
				dbfx.Exec(t, "UPDATE agent_task_queue SET is_leader_task=false WHERE id=$1", sourceID)
			}
			if order == "historical" {
				completeWorkerReplyRun(t, first.ID)
			}
			wakes := service.IssueWakeupService{Tasks: testHandler.TaskService}
			if order == "capacity" {
				for n := 0; n < 32; n++ {
					_, err := wakes.Create(ctx, parseUUID(issueID), parseUUID(testUserID), parseUUID(first.ID), service.WakeupInput{AgentID: leaderID, Kind: "event", EventTypes: []string{"comment.created"}, Instruction: "Capacity fixture"})
					if err != nil {
						t.Fatal(err)
					}
				}
				req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/squad-evaluated", map[string]any{"outcome": "no_action", "reason": "Waiting for completion"}), "id", issueID)
				req.Header.Set("X-Agent-ID", leaderID)
				req.Header.Set("X-Task-ID", first.ID)
				testutil.Call(t, testHandler.RecordSquadLeaderEvaluation, req).Want(http.StatusBadRequest)
				if n := dbfx.Count(t, "SELECT count(*) FROM activity_log WHERE issue_id=$1 AND action='squad_leader_evaluated'", issueID); n != 0 {
					t.Fatal("capacity failure committed evaluation without its wait")
				}
				if n := dbfx.Count(t, "SELECT count(*) FROM issue_wakeup WHERE issue_id=$1 AND filter_task_id=$2", issueID, workerTaskID); n != 0 {
					t.Fatal("capacity failure committed partial completion waits")
				}
				return
			}
			if order == "contention" {
				holder, err := testPool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = holder.Exec(ctx, "SELECT id FROM agent_task_queue WHERE id=$1 FOR UPDATE", workerTaskID); err != nil {
					t.Fatal(err)
				}
				defer holder.Rollback(ctx)
				done := make(chan error, 1)
				go func() {
					time.Sleep(15 * time.Millisecond)
					_, e := holder.Exec(ctx, "UPDATE agent_task_queue SET status='completed',completed_at=now() WHERE id=$1", workerTaskID)
					if e == nil {
						e = holder.Commit(ctx)
					}
					done <- e
				}()
				recordSquadLeaderEvaluationForTask(t, fx, "no_action")
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			} else {
				recordSquadLeaderEvaluationForTask(t, fx, "no_action")
			}
			if order == "disabled" || order == "deleted" || order == "reenabled" {
				var id string
				dbfx.QueryRow(t, "SELECT id FROM issue_wakeup WHERE source_task_id=$1", first.ID).Scan(&id)
				if order == "disabled" || order == "reenabled" {
					disabled, err := wakes.Disable(ctx, parseUUID(issueID), parseUUID(id), parseUUID(testUserID))
					if err != nil {
						t.Fatal(err)
					}
					if order == "reenabled" {
						if _, err := wakes.Enable(ctx, parseUUID(issueID), parseUUID(testUserID), pgtype.UUID{}, disabled.ID, service.WakeupEnableInput{Revision: disabled.Revision}); err != nil {
							t.Fatal(err)
						}
					}
				} else if err := wakes.Delete(ctx, parseUUID(issueID), parseUUID(id), parseUUID(testUserID)); err != nil {
					t.Fatal(err)
				}
			}
			recordSquadLeaderEvaluationForTask(t, fx, "no_action")
			if order == "while_leader" {
				completeWorkerReplyRun(t, workerTaskID)
			}
			if order != "historical" {
				completeWorkerReplyRun(t, first.ID)
			}
			var waitingID string
			if order == "ordinary_queued" || order == "leader_queued" {
				waitingID = dbfx.Task(t, leaderID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "originator_user_id": testUserID, "accountable_user_id": testUserID, "is_leader_task": order == "leader_queued", "squad_id": squadID})
			}
			if order != "before_evaluation" && order != "while_leader" && order != "before_claim" && order != "contention" && order != "completion_commit_after_claim" {
				if order == "failed" || order == "cancelled" {
					dbfx.Exec(t, "UPDATE agent_task_queue SET status=$2,completed_at=now() WHERE id=$1", workerTaskID, order)
				} else {
					completeWorkerReplyRun(t, workerTaskID)
				}
			}
			if order == "rotated" {
				dbfx.Exec(t, "UPDATE squad SET leader_id=$2 WHERE id=$1", squadID, workerID)
			}
			if order == "closed" {
				setStatusAsRun(t, issueID, "done", "", "")
			}
			if err := wakes.TickWorkspaces(ctx, parseUUID(testWorkspaceID)); err != nil {
				t.Fatal(err)
			}
			if order == "disabled" || order == "deleted" || order == "rotated" || order == "closed" || order == "wrong_source" || order == "historical" {
				if next := claimSquadCompletionRun(t, runtimeID); next != nil {
					t.Fatal("stopped or unrelated work must not create completion continuation")
				}
				return
			}
			if order == "ordinary_queued" {
				claimed := claimSquadCompletionRun(t, runtimeID)
				if claimed == nil || claimed.ID != waitingID {
					t.Fatal("expected ordinary run before completion wakeup")
				}
				ordinary, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(waitingID))
				if err != nil {
					t.Fatal(err)
				}
				joined, err := wakes.JoinWaitingWakeups(ctx, ordinary)
				if err != nil {
					t.Fatal(err)
				}
				if len(service.JoinedWakeupNotes(joined)) != 0 || string(joined) != string(ordinary.Context) {
					t.Fatal("ordinary run consumed a leader completion wakeup")
				}
				dbfx.Exec(t, "UPDATE agent_task_queue SET status='cancelled',completed_at=now() WHERE id=$1", waitingID)
			}
			next := claimSquadCompletionRun(t, runtimeID)
			if next == nil || !next.IsLeaderTask {
				t.Fatal("consumed worker result followed by runtime completion must wake a squad leader")
			}
			if order == "leader_queued" && next.ID != waitingID {
				t.Fatal("completion should join the already queued leader")
			}
			stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(next.ID))
			if err != nil {
				t.Fatal(err)
			}
			if stored.SquadID != parseUUID(squadID) || stored.OriginatorUserID != parseUUID(testUserID) {
				t.Fatal("completion lost squad or human provenance")
			}
			if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(next.ID)); err != nil {
				t.Fatal(err)
			}
			completeWorkerReplyRun(t, next.ID)
			if secondWorker != "" {
				completeWorkerReplyRun(t, secondWorker)
				if err := wakes.TickWorkspaces(ctx, parseUUID(testWorkspaceID)); err != nil {
					t.Fatal(err)
				}
				second := claimSquadCompletionRun(t, runtimeID)
				if second == nil || !second.IsLeaderTask {
					t.Fatal("second merged worker completion was lost")
				}
				if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(second.ID)); err != nil {
					t.Fatal(err)
				}
				completeWorkerReplyRun(t, second.ID)
			}
			if err := wakes.TickWorkspaces(ctx, parseUUID(testWorkspaceID)); err != nil {
				t.Fatal(err)
			}
			if another := claimSquadCompletionRun(t, runtimeID); another != nil {
				t.Fatal("completion was delivered more than once")
			}
			want := 1
			if secondWorker != "" {
				want = 2
			}
			if n := dbfx.Count(t, "SELECT count(*) FROM issue_wakeup WHERE source_task_id=$1 AND NOT enabled", first.ID); n != want {
				t.Fatalf("completion subscriptions should be consumed once each, got %d", n)
			}
		})
	}
}

func claimSquadCompletionRun(t *testing.T, runtimeID string) *AgentTaskResponse {
	t.Helper()
	req := withURLParam(newOwnerDaemonRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil), "runtimeId", runtimeID)
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityCoalescedCommentsV1+","+protocol.DaemonCapabilityJoinedWakeupsV1)
	var response struct {
		Task *AgentTaskResponse `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&response)
	return response.Task
}
