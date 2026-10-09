package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func claimWorkerReplyRun(t *testing.T, runtimeID string) *AgentTaskResponse {
	t.Helper()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "worker-reply-handoff")
	req = withURLParam(req, "runtimeId", runtimeID)
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityCoalescedCommentsV1)
	var response struct {
		Task *AgentTaskResponse `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&response)
	return response.Task
}

func completeWorkerReplyRun(t *testing.T, taskID string) {
	t.Helper()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/complete", map[string]any{"output": "Processed the inputs delivered to this run"}, testWorkspaceID, "worker-reply-handoff")
	req = withURLParam(req, "taskId", taskID)
	testutil.Call(t, testHandler.CompleteTask, req).Want(http.StatusOK)
}

// A worker progress comment wakes the leader; its final reply must also reach a
// leader run even if the first wake was claimed before the reply arrived.
func TestWorkerReplyDelivery(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, state := range []string{"queued", "dispatched", "running"} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/explicit=%t", state, explicit), func(t *testing.T) {
				ctx := context.Background()
				leaderRuntimeID := dbfx.Runtime(t, "Worker handoff leader runtime")
				leaderID := dbfx.Agent(t, "Worker handoff leader", leaderRuntimeID, testutil.Cols{"max_concurrent_tasks": 3})
				workerRuntimeID := dbfx.Runtime(t, "Worker handoff worker runtime")
				workerID := dbfx.Agent(t, "Worker handoff worker", workerRuntimeID)
				squadID := dbfx.Squad(t, "Worker handoff squad", leaderID)
				dbfx.SquadMember(t, squadID, "agent", workerID)
				issueID := dbfx.Issue(t, "Worker results must reach the coordinator", testutil.Cols{
					"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
				})
				sourceID := dbfx.Task(t, leaderID, testutil.Cols{
					"runtime_id": leaderRuntimeID, "issue_id": issueID, "status": "completed",
					"is_leader_task": true, "squad_id": squadID,
					"originator_user_id": testUserID, "accountable_user_id": testUserID,
				})
				rootID := dbfx.Comment(t, issueID, fmt.Sprintf("[@Worker](mention://agent/%s) verify the implementation", workerID), testutil.Cols{
					"author_type": "agent", "author_id": leaderID, "source_task_id": sourceID,
				})
				workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
					"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
					"trigger_comment_id": rootID, "squad_id": squadID, "delegated_from_task_id": sourceID,
					"originator_user_id": testUserID, "accountable_user_id": testUserID,
				})
				post := func(content string) CommentResponse {
					req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
						"content": content, "parent_id": rootID,
					}), "id", issueID)
					req.Header.Set("X-Agent-ID", workerID)
					req.Header.Set("X-Task-ID", workerTaskID)
					var response CommentResponse
					testutil.Call(t, testHandler.CreateComment, req).Want(http.StatusCreated).JSON(&response)
					if response.AuthorType != "agent" || response.SourceTaskID == nil || *response.SourceTaskID != workerTaskID {
						t.Fatal("worker reply lost its authenticated source task")
					}
					return response
				}
				progress := post("Verification started; final results will follow in this thread")
				var leaderTaskID string
				dbfx.QueryRow(t, `SELECT id FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'`, issueID, leaderID).Scan(&leaderTaskID)
				var first *AgentTaskResponse
				if state != "queued" {
					first = claimWorkerReplyRun(t, leaderRuntimeID)
					if first == nil || first.ID != leaderTaskID || !slices.Contains(first.DeliveredCommentIDs, progress.ID) {
						t.Fatal("first leader run did not receive the worker's progress")
					}
				}
				if state == "running" {
					if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(leaderTaskID)); err != nil {
						t.Fatal(err)
					}
				}
				content := "Verification complete: found a correctness issue; please coordinate the repair"
				if explicit {
					content = fmt.Sprintf("[@Squad](mention://squad/%s) %s", squadID, content)
				}
				details := post("Evidence: the final reply can arrive after the leader claims its inputs")
				result := post(content)
				stored, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(leaderTaskID))
				if err != nil {
					t.Fatal(err)
				}
				if state == "dispatched" && !explicit {
					for _, id := range []string{details.ID, result.ID} {
						if !slices.Contains(stored.CoalescedCommentIds, parseUUID(id)) || slices.Contains(stored.DeliveredCommentIds, parseUUID(id)) {
							t.Fatal("accepted worker reply must be planned without changing the earlier delivery receipt")
						}
					}
					if stored.TriggerCommentID != parseUUID(progress.ID) {
						t.Fatal("registering a worker reply must not replace an already claimed trigger")
					}
				}
				if state == "queued" {
					first = claimWorkerReplyRun(t, leaderRuntimeID)
					if first == nil || first.ID != leaderTaskID || !slices.Contains(first.DeliveredCommentIDs, result.ID) || !slices.Contains(first.DeliveredCommentIDs, details.ID) || first.TriggerCommentContent != content {
						t.Fatal("queued leader must coalesce and receive the worker result")
					}
				} else if slices.Contains(first.DeliveredCommentIDs, result.ID) {
					t.Fatal("an earlier claim cannot have delivered a later comment")
				}
				if state != "running" {
					if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(leaderTaskID)); err != nil {
						t.Fatal(err)
					}
				}
				if other := claimWorkerReplyRun(t, leaderRuntimeID); other != nil {
					t.Fatal("same issue/leader must remain serialized")
				}
				completeWorkerReplyRun(t, leaderTaskID)
				next := claimWorkerReplyRun(t, leaderRuntimeID)
				if state == "queued" {
					if next != nil {
						t.Fatal("already delivered result must not create another leader run")
					}
					return
				}
				if next == nil {
					t.Fatal("worker result persisted but was neither delivered nor followed by another leader run")
				}
				if !next.IsLeaderTask || !slices.Contains(next.DeliveredCommentIDs, result.ID) || !slices.Contains(next.DeliveredCommentIDs, details.ID) || next.TriggerCommentContent != content {
					t.Fatal("follow-up must deliver the result in the squad leader role")
				}
				successor, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(next.ID))
				if err != nil {
					t.Fatal(err)
				}
				if successor.SquadID != parseUUID(squadID) || successor.OriginatorUserID != parseUUID(testUserID) || successor.AccountableUserID != parseUUID(testUserID) || successor.DelegatedFromTaskID != parseUUID(workerTaskID) {
					t.Fatal("worker follow-up lost squad or human delegation provenance")
				}
				if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(next.ID)); err != nil {
					t.Fatal(err)
				}
				completeWorkerReplyRun(t, next.ID)
				if another := claimWorkerReplyRun(t, leaderRuntimeID); another != nil {
					t.Fatal("worker result must not generate an endless leader follow-up loop")
				}
			})
		}
	}
}

// Unplanned replies must be in the completing run's thread and timestamp window:
// otherwise a passing negative case could merely be the SQL scope excluding it.
func TestWorkerReplyReconcileBoundaries(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	for _, mode := range []string{"accepted", "unplanned", "suppressed", "leader_reply", "note", "archived_leader", "reassigned", "registration_failure", "completed_before_registration"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			runtimeID := dbfx.Runtime(t, "Worker replay boundary leader")
			leaderID := dbfx.Agent(t, "Worker replay boundary leader", runtimeID)
			workerRuntimeID := dbfx.Runtime(t, "Worker replay boundary worker")
			workerID := dbfx.Agent(t, "Worker replay boundary worker", workerRuntimeID)
			squadID := dbfx.Squad(t, "Worker replay boundary squad", leaderID)
			dbfx.SquadMember(t, squadID, "agent", workerID)
			issueID := dbfx.Issue(t, "Worker replay boundary", testutil.Cols{
				"status": "in_progress", "assignee_type": "squad", "assignee_id": squadID,
			})
			rootID := dbfx.Comment(t, issueID, "Coordinate this work", testutil.Cols{"created_at": testutil.Raw("now() - interval '6 minutes'")})
			taskID := dbfx.Task(t, leaderID, testutil.Cols{
				"runtime_id": runtimeID, "issue_id": issueID, "status": "queued",
				"trigger_comment_id": rootID, "is_leader_task": true, "squad_id": squadID,
				"originator_user_id": testUserID, "accountable_user_id": testUserID,
				"created_at": testutil.Raw("now() - interval '5 minutes'"),
			})
			workerTaskID := dbfx.Task(t, workerID, testutil.Cols{
				"runtime_id": workerRuntimeID, "issue_id": issueID, "status": "running",
				"trigger_comment_id": rootID, "squad_id": squadID,
				"originator_user_id": testUserID, "accountable_user_id": testUserID,
			})
			first := claimWorkerReplyRun(t, runtimeID)
			if first == nil || first.ID != taskID {
				t.Fatal("leader run was not claimed")
			}
			var replyID string
			if mode == "unplanned" {
				replyID = dbfx.Comment(t, issueID, "An ordinary reply with no accepted dispatch", testutil.Cols{
					"parent_id": rootID, "author_type": "agent", "author_id": workerID, "source_task_id": workerTaskID,
				})
			} else {
				body := map[string]any{"content": "The worker has results", "parent_id": rootID}
				if mode == "note" {
					body["content"] = "/note an informational update"
				}
				if mode == "suppressed" {
					body["suppress_agent_ids"] = []string{leaderID}
				}
				req := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", body), "id", issueID)
				req.Header.Set("X-Agent-ID", workerID)
				req.Header.Set("X-Task-ID", workerTaskID)
				if mode == "leader_reply" {
					req.Header.Set("X-Agent-ID", leaderID)
					req.Header.Set("X-Task-ID", taskID)
				}
				var response CommentResponse
				h := *testHandler
				registration := &workerReplyRegistrationDB{DBTX: testPool}
				if mode == "registration_failure" {
					registration.err = errors.New("injected worker reply registration failure")
				}
				if mode == "completed_before_registration" {
					registration.before = func() {
						if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(taskID)); err != nil {
							t.Fatal(err)
						}
						completeWorkerReplyRun(t, taskID)
					}
				}
				if mode == "registration_failure" || mode == "completed_before_registration" {
					h.Queries = db.New(registration)
				}
				testutil.Call(t, h.CreateComment, req).Want(http.StatusCreated).JSON(&response)
				if (mode == "registration_failure" || mode == "completed_before_registration") && registration.calls != 1 {
					t.Fatalf("registration probe called %d times", registration.calls)
				}
				replyID = response.ID
			}
			task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			comments, err := testHandler.Queries.ListReconcilableCommentsForIssueSince(ctx, db.ListReconcilableCommentsForIssueSinceParams{
				IssueID: task.IssueID, CommentThreadID: task.CommentThreadID, Since: task.CreatedAt,
				PlannedCommentIds: task.CoalescedCommentIds,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.ContainsFunc(comments, func(c db.Comment) bool { return c.ID == parseUUID(replyID) }) {
				t.Fatal("reply must reach the replay filter, not be excluded by the SQL thread/time window")
			}
			wantPlanned := mode == "accepted" || mode == "archived_leader" || mode == "reassigned"
			if slices.Contains(task.CoalescedCommentIds, parseUUID(replyID)) != wantPlanned {
				t.Fatalf("unexpected creation-time obligation for %s", mode)
			}
			if mode == "archived_leader" {
				dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, leaderID)
			}
			if mode == "reassigned" {
				dbfx.Exec(t, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, workerID)
			}
			if mode != "completed_before_registration" {
				if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(taskID)); err != nil {
					t.Fatal(err)
				}
				completeWorkerReplyRun(t, taskID)
			}
			queued := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'`, issueID)
			if mode != "accepted" && mode != "completed_before_registration" {
				if queued != 0 {
					t.Fatalf("%s must not create a completion-driven run, got %d", mode, queued)
				}
				return
			}
			if queued != 1 {
				t.Fatalf("accepted reply must create exactly one successor, got %d", queued)
			}
			next := claimWorkerReplyRun(t, runtimeID)
			if next == nil || !slices.Contains(next.DeliveredCommentIDs, replyID) {
				t.Fatal("accepted reply was not delivered")
			}
			if _, err := testHandler.TaskService.StartTask(ctx, parseUUID(next.ID)); err != nil {
				t.Fatal(err)
			}
			completeWorkerReplyRun(t, next.ID)
			if extra := claimWorkerReplyRun(t, runtimeID); extra != nil {
				t.Fatal("delivered worker reply replayed again")
			}
		})
	}
}

// If completion commits while registration waits for the task row lock, the
// caller must see a miss and enqueue fresh, not attach an obligation to a run
// whose completion snapshot can no longer include it.
func TestWorkerReplyRegistrationLosesCompletionRace(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	runtimeID := dbfx.Runtime(t, "Worker registration race")
	agentID := dbfx.Agent(t, "Worker registration race", runtimeID)
	issueID := dbfx.Issue(t, "Worker registration race")
	rootID := dbfx.Comment(t, issueID, "Initial input")
	replyID := dbfx.Comment(t, issueID, "Worker result", testutil.Cols{"parent_id": rootID})
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "running", "trigger_comment_id": rootID,
	})
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Error(err)
		}
	}()
	if _, err := tx.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, taskID); err != nil {
		t.Fatal(err)
	}
	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	pid := conn.Conn().PgConn().PID()
	done := make(chan struct{})
	var registrationErr error
	go func() {
		_, registrationErr = db.New(conn).RegisterPlannedCommentForActiveTask(ctx, db.RegisterPlannedCommentForActiveTaskParams{
			IssueID: parseUUID(issueID), AgentID: parseUUID(agentID), CommentID: parseUUID(replyID),
		})
		close(done)
	}()
	// Stop the registration query before returning its connection to the pool,
	// including assertion failures while it is waiting for the task row lock.
	defer func() { cancel(); <-done }()
	for {
		var blocked bool
		if err := testPool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, pid).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("registration did not block on the completing row")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	<-done
	if !errors.Is(registrationErr, pgx.ErrNoRows) {
		t.Fatalf("registration after completion = %v, want no rows", registrationErr)
	}
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(task.CoalescedCommentIds, parseUUID(replyID)) {
		t.Fatal("completed run acquired an undeliverable obligation")
	}
}

// Intercept only the new obligation write; all routing, comment persistence,
// completion, and successor enqueue still use their real database paths.
type workerReplyRegistrationDB struct {
	db.DBTX
	before func()
	err    error
	calls  int
}

func (d *workerReplyRegistrationDB) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if strings.Contains(query, "-- name: RegisterPlannedCommentForActiveTask :one") {
		d.calls++
		if d.before != nil {
			d.before()
		}
		if d.err != nil {
			return errRow{err: d.err}
		}
	}
	return d.DBTX.QueryRow(ctx, query, args...)
}
