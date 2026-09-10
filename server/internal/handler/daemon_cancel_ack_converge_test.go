package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// A daemon only acks after its run has stopped. These tests cover what the
// server does with that fact when its own row never reached a terminal state —
// the second half of GH #8272, where such a row stayed 'running' forever
// because the stale-task sweeper skips rows whose runtime is still
// heartbeating.

func ackRequest(t *testing.T, taskID string, body map[string]any) *http.Request {
	t.Helper()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/cancel-ack", body, testWorkspaceID, "test-daemon")
	return withURLParam(req, "taskId", taskID)
}

// TestAckTaskCancelled_ConvergesAbandonedRunningTask is the exact shape the
// reproduction probe pinned: a long-running row, a live heartbeat, an ack that
// used to return 200 and change nothing.
func TestAckTaskCancelled_ConvergesAbandonedRunningTask(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "MUL-7259 converge runtime")
	agentID := dbfx.Agent(t, "MUL-7259 converge agent", runtimeID)
	issueID := dbfx.Issue(t, "MUL-7259 converge issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
		"started_at": testutil.Raw("now() - interval '5 hours'"),
		"attempt":    1, "max_attempts": 2,
	})

	testutil.Call(t, testHandler.AckTaskCancelled, ackRequest(t, taskID, map[string]any{
		"branch_name": "fix/mul7259-preserved-work", "durable_work_dir": "/test/preserved",
	})).Want(http.StatusOK)

	q := testHandler.Queries
	task, err := q.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "failed" {
		t.Fatalf("status = %q, want failed — an abandoned row must not stay running", task.Status)
	}
	if task.FailureReason.String != string(taskfailure.ReasonRuntimeAbandoned) {
		t.Fatalf("failure_reason = %q, want %q", task.FailureReason.String, taskfailure.ReasonRuntimeAbandoned)
	}
	if !task.CompletedAt.Valid {
		t.Fatal("completed_at must be set so the row reads as finished")
	}
	// The branch is the only pointer to what the run committed. Convergence
	// writes it in the same statement, because the pointer CASes below it in the
	// handler key on status='cancelled' and would miss the row we just failed.
	if task.BranchName.String != "fix/mul7259-preserved-work" {
		t.Fatalf("branch_name = %q, want the delivered branch", task.BranchName.String)
	}
	if task.DurableWorkDir.String != "/test/preserved" {
		t.Fatalf("durable_work_dir = %q, want the delivered workdir", task.DurableWorkDir.String)
	}
}

// TestAckTaskCancelled_ReleasesCapacityAndAgent covers the damage the stuck row
// actually did: it counted against max_concurrent_tasks forever and pinned the
// agent at 'working'. Rarity never protected anyone here, because the loss
// accumulated across incidents.
func TestAckTaskCancelled_ReleasesCapacityAndAgent(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "MUL-7259 capacity runtime")
	agentID := dbfx.Agent(t, "MUL-7259 capacity agent", runtimeID)
	issueID := dbfx.Issue(t, "MUL-7259 capacity issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
		"started_at": testutil.Raw("now() - interval '5 hours'"),
		"attempt":    1, "max_attempts": 2,
	})

	q := testHandler.Queries
	if n, err := q.CountRunningTasks(ctx, parseUUID(agentID)); err != nil || n != 1 {
		t.Fatalf("precondition: running capacity = %d, err = %v, want 1", n, err)
	}

	testutil.Call(t, testHandler.AckTaskCancelled, ackRequest(t, taskID, map[string]any{})).Want(http.StatusOK)

	if n, err := q.CountRunningTasks(ctx, parseUUID(agentID)); err != nil || n != 0 {
		t.Fatalf("running capacity = %d, err = %v, want 0 — the slot must come back", n, err)
	}
	agent, err := q.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatal(err)
	}
	if agent.Status != "idle" {
		t.Fatalf("agent status = %q, want idle", agent.Status)
	}
}

// TestAckTaskCancelled_SpendsRemainingRetry: converging to 'failed' rather than
// 'cancelled' is what makes this a recovery. Retries are gated on 'failed' plus
// a retryable reason, so a 'cancelled' convergence would tidy the row and still
// lose the run.
func TestAckTaskCancelled_SpendsRemainingRetry(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "MUL-7259 retry runtime")
	agentID := dbfx.Agent(t, "MUL-7259 retry agent", runtimeID)
	issueID := dbfx.Issue(t, "MUL-7259 retry issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
		"started_at": testutil.Raw("now() - interval '5 hours'"),
		"attempt":    1, "max_attempts": 2,
	})

	testutil.Call(t, testHandler.AckTaskCancelled, ackRequest(t, taskID, map[string]any{})).Want(http.StatusOK)

	tasks, err := testHandler.Queries.ListTasksByIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	var retry *db.AgentTaskQueue
	for i := range tasks {
		if uuidToString(tasks[i].ID) != taskID {
			retry = &tasks[i]
		}
	}
	if retry == nil {
		t.Fatal("no retry was enqueued; the remaining attempt was lost")
	}
	if retry.Attempt != 2 {
		t.Fatalf("retry attempt = %d, want 2", retry.Attempt)
	}
}

// TestAckTaskCancelled_ExhaustedBudgetDoesNotRetry: convergence must not invent
// retries beyond the existing rules.
func TestAckTaskCancelled_ExhaustedBudgetDoesNotRetry(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "MUL-7259 budget runtime")
	agentID := dbfx.Agent(t, "MUL-7259 budget agent", runtimeID)
	issueID := dbfx.Issue(t, "MUL-7259 budget issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
		"started_at": testutil.Raw("now() - interval '5 hours'"),
		"attempt":    2, "max_attempts": 2,
	})

	testutil.Call(t, testHandler.AckTaskCancelled, ackRequest(t, taskID, map[string]any{})).Want(http.StatusOK)

	tasks, err := testHandler.Queries.ListTasksByIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1 — an exhausted budget must not produce a retry", len(tasks))
	}
}

// TestAckTaskCancelled_UserCancelStaysCancelled is the safety property the whole
// design rests on. A user cancel writes 'cancelled' before the daemon acks, so
// the CAS must refuse and the row must keep reading as a cancellation, not a
// failure — and must not be retried.
func TestAckTaskCancelled_UserCancelStaysCancelled(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "MUL-7259 usercancel runtime")
	agentID := dbfx.Agent(t, "MUL-7259 usercancel agent", runtimeID)
	issueID := dbfx.Issue(t, "MUL-7259 usercancel issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "cancelled",
		"started_at": testutil.Raw("now() - interval '1 hour'"),
		"attempt":    1, "max_attempts": 2,
	})

	testutil.Call(t, testHandler.AckTaskCancelled, ackRequest(t, taskID, map[string]any{
		"branch_name": "fix/mul7259-user-cancel", "error_message": "worktree preserved",
		"failure_reason": "local_directory_error",
	})).Want(http.StatusOK)

	q := testHandler.Queries
	task, err := q.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "cancelled" {
		t.Fatalf("status = %q, want cancelled — a user cancel must never be rewritten as a failure", task.Status)
	}
	// The pre-existing record-only path must still deliver its pointers.
	if task.BranchName.String != "fix/mul7259-user-cancel" {
		t.Fatalf("branch_name = %q, want the delivered branch", task.BranchName.String)
	}
	if task.Error.String != "worktree preserved" {
		t.Fatalf("error = %q, want the preserved-work pointer", task.Error.String)
	}
	tasks, err := q.ListTasksByIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1 — a user cancel must not be retried", len(tasks))
	}
}

// TestAckTaskCancelled_CompletedTaskIsUntouched: a completion that raced ahead of
// the ack owns the row. Convergence must not downgrade a success.
func TestAckTaskCancelled_CompletedTaskIsUntouched(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "MUL-7259 completed runtime")
	agentID := dbfx.Agent(t, "MUL-7259 completed agent", runtimeID)
	issueID := dbfx.Issue(t, "MUL-7259 completed issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "completed",
		"started_at": testutil.Raw("now() - interval '1 hour'"),
		"attempt":    1, "max_attempts": 2,
	})

	testutil.Call(t, testHandler.AckTaskCancelled, ackRequest(t, taskID, map[string]any{})).Want(http.StatusOK)

	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "completed" {
		t.Fatalf("status = %q, want completed", task.Status)
	}
}

// TestAckTaskCancelled_ReplayIsIdempotent: the daemon retries this ack, and the
// durable outbox replays it after a restart, so at-least-once delivery must not
// produce a second failure, a second retry, or a changed row.
func TestAckTaskCancelled_ReplayIsIdempotent(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "MUL-7259 replay runtime")
	agentID := dbfx.Agent(t, "MUL-7259 replay agent", runtimeID)
	issueID := dbfx.Issue(t, "MUL-7259 replay issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
		"started_at": testutil.Raw("now() - interval '5 hours'"),
		"attempt":    1, "max_attempts": 2,
	})

	for i := 0; i < 3; i++ {
		testutil.Call(t, testHandler.AckTaskCancelled, ackRequest(t, taskID, map[string]any{
			"branch_name": "fix/mul7259-replay",
		})).Want(http.StatusOK)
	}

	q := testHandler.Queries
	task, err := q.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != "failed" || task.FailureReason.String != string(taskfailure.ReasonRuntimeAbandoned) {
		t.Fatalf("status=%q reason=%q, want failed/%s", task.Status, task.FailureReason.String, taskfailure.ReasonRuntimeAbandoned)
	}
	tasks, err := q.ListTasksByIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	// Original + exactly one retry, however many times the ack was replayed.
	if len(tasks) != 2 {
		t.Fatalf("got %d tasks, want 2 — replayed acks must not fan out retries", len(tasks))
	}
}

// TestAckTaskCancelled_DaemonReportedReasonWins: when the daemon knows why it
// stopped, that reason must survive. local_directory_error is deliberately not
// retryable — the same local problem would recur — so this also proves the retry
// decision follows the reason rather than the convergence path.
func TestAckTaskCancelled_DaemonReportedReasonWins(t *testing.T) {
	ctx := context.Background()
	runtimeID := dbfx.Runtime(t, "MUL-7259 reason runtime")
	agentID := dbfx.Agent(t, "MUL-7259 reason agent", runtimeID)
	issueID := dbfx.Issue(t, "MUL-7259 reason issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID, "issue_id": issueID, "status": "running",
		"started_at": testutil.Raw("now() - interval '5 hours'"),
		"attempt":    1, "max_attempts": 2,
	})

	testutil.Call(t, testHandler.AckTaskCancelled, ackRequest(t, taskID, map[string]any{
		"error_message": "worktree preserved at /tmp/wt", "failure_reason": "local_directory_error",
	})).Want(http.StatusOK)

	q := testHandler.Queries
	task, err := q.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if task.FailureReason.String != "local_directory_error" {
		t.Fatalf("failure_reason = %q, want the daemon's own reason", task.FailureReason.String)
	}
	if task.Error.String != "worktree preserved at /tmp/wt" {
		t.Fatalf("error = %q, want the preserved-work pointer", task.Error.String)
	}
	tasks, err := q.ListTasksByIssue(ctx, parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1 — a non-retryable reason must not be retried", len(tasks))
	}
}
