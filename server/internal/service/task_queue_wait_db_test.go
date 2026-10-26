package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DB-backed lifecycle coverage for per-task queue-wait attribution (route B):
// what a poll writes, what a claim clears, and that attribution never
// jeopardizes the claim itself. Reuses the claim-decision fixture (shared pool
// convention: skips without a reachable Postgres).

// queueWaitReasonOf reads the persisted attribution, NULL-aware.
func queueWaitReasonOf(t *testing.T, pool *pgxpool.Pool, taskID string) *string {
	t.Helper()
	var reason *string
	if err := pool.QueryRow(context.Background(),
		`SELECT queue_wait_reason FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&reason); err != nil {
		t.Fatalf("read queue_wait_reason for %s: %v", taskID, err)
	}
	return reason
}

func wantReason(t *testing.T, pool *pgxpool.Pool, taskID, want string) {
	t.Helper()
	got := queueWaitReasonOf(t, pool, taskID)
	switch {
	case want == "" && got != nil:
		t.Fatalf("task %s queue_wait_reason = %q, want NULL", taskID, *got)
	case want != "" && (got == nil || *got != want):
		actual := "<NULL>"
		if got != nil {
			actual = *got
		}
		t.Fatalf("task %s queue_wait_reason = %q, want %q", taskID, actual, want)
	}
}

// TestQueueWaitDBAgentCapacityClearsOnClaim is the #8551 display-half
// lifecycle: with max_concurrent_tasks=1 and two queued tasks, the first poll
// claims the head (clearing any attribution it carried) and leaves the tail
// untouched; once the head holds the agent's only slot, the next poll records
// agent_capacity on the tail; when the tail is finally claimable, the claim
// clears it again.
func TestQueueWaitDBAgentCapacityClearsOnClaim(t *testing.T) {
	f := newClaimDecisionFixture(t, 1)
	otherIssue := f.fx.Issue(t, "queue wait second issue")
	head := f.insertTask(t, "queued")
	tail := f.fx.Task(t, util.UUIDToString(f.agentID), testutil.Cols{
		"runtime_id": util.UUIDToString(f.runtimeID),
		"issue_id":   otherIssue,
		"status":     "queued",
	})

	// Simulate an attribution an earlier poll wrote for the head, so the
	// claim-clear is observable on the very first poll.
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE agent_task_queue SET queue_wait_reason = 'attempted_unclaimed' WHERE id = $1`, head,
	); err != nil {
		t.Fatalf("seed head attribution: %v", err)
	}

	ctx := context.Background()
	claimed, err := f.svc.ClaimTaskForRuntime(ctx, f.runtimeID)
	if err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if claimed == nil || util.UUIDToString(claimed.ID) != head {
		t.Fatalf("first poll claimed %v, want head %s", claimed, head)
	}
	// Leaving the queue clears the column — a later read must not mistake
	// "previously waited" for "currently waiting".
	wantReason(t, f.pool, head, "")
	// The tail was never attempted by this poll (the loop stops at the first
	// successful agent claim), so it keeps no attribution.
	wantReason(t, f.pool, tail, "")

	// The dispatched head now occupies the agent's only slot: the next poll
	// sees the tail, fails the capacity check, and persists agent_capacity.
	second, err := f.svc.ClaimTaskForRuntime(ctx, f.runtimeID)
	if err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if second != nil {
		t.Fatalf("second poll claimed %s, want nil", util.UUIDToString(second.ID))
	}
	wantReason(t, f.pool, tail, queueWaitAgentCapacity)

	// Third phase, promised above: settle the head so the tail becomes
	// claimable, and the claim must clear the tail's attribution on its way
	// out of 'queued' — a later read must not mistake the recorded wait for a
	// live one.
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE agent_task_queue SET status = 'completed', completed_at = now(), prepare_lease_expires_at = NULL WHERE id = $1`, head,
	); err != nil {
		t.Fatalf("settle head: %v", err)
	}
	third, err := f.svc.ClaimTaskForRuntime(ctx, f.runtimeID)
	if err != nil {
		t.Fatalf("third poll: %v", err)
	}
	if third == nil || util.UUIDToString(third.ID) != tail {
		t.Fatalf("third poll claimed %v, want tail %s", third, tail)
	}
	wantReason(t, f.pool, tail, "")
}

// TestQueueWaitDBAttemptedUnclaimedHeadOnly pins the runtime-offline shape of
// attempted_unclaimed: the candidate stays visible (the candidate query has no
// liveness fence) while the claim is rejected, and only the head row the poll
// actually attempted is attributed — the rows behind it earned no claim.
func TestQueueWaitDBAttemptedUnclaimedHeadOnly(t *testing.T) {
	f := newClaimDecisionFixture(t, 5)
	otherIssue := f.fx.Issue(t, "queue wait other issue")
	head := f.insertTask(t, "queued")
	tail := f.fx.Task(t, util.UUIDToString(f.agentID), testutil.Cols{
		"runtime_id": util.UUIDToString(f.runtimeID),
		"issue_id":   otherIssue,
		"status":     "queued",
	})

	if _, err := f.pool.Exec(context.Background(),
		`UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, f.runtimeID,
	); err != nil {
		t.Fatalf("take runtime offline: %v", err)
	}

	claimed, err := f.svc.ClaimTaskForRuntime(context.Background(), f.runtimeID)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if claimed != nil {
		t.Fatalf("poll claimed %s, want nil", util.UUIDToString(claimed.ID))
	}
	wantReason(t, f.pool, head, queueWaitAttemptedUnclaimed)
	wantReason(t, f.pool, tail, "")
}

// queueWaitUpdateTracer counts executions of the UpdateQueueWaitReasons
// statement on its pool. The steady-state guarantee is "no statement sent",
// which the persisted value alone cannot prove: an UPDATE whose IS DISTINCT
// FROM guard matched zero rows also leaves the value unchanged. Only the wire
// distinguishes the two.
type queueWaitUpdateTracer struct{ updates int }

func (tr *queueWaitUpdateTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	// sqlc keeps its "-- name: UpdateQueueWaitReasons :exec" marker as the
	// first line of the statement text, so the marker names this query and no
	// other statement in the claim path.
	if strings.Contains(data.SQL, "UpdateQueueWaitReasons") {
		tr.updates++
	}
	return ctx
}

func (tr *queueWaitUpdateTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {}

// tracedQueueWaitPool opens a dedicated pool (same DATABASE_URL as the shared
// one) whose connections report every query to the tracer. A separate pool is
// required because the shared pool's tracer would count other tests' writes.
func tracedQueueWaitPool(t *testing.T, tracer pgx.QueryTracer) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	cfg.ConnConfig.Tracer = tracer
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open traced pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestQueueWaitDBSteadyStateNoRewrite covers the steady state: a second poll
// with the same outcome must not rewrite the row. The pure test pins the
// diffing logic (a row already carrying the reason yields no update); here a
// query tracer on a dedicated pool proves the end-to-end zero-write behavior —
// the first poll issues exactly one attribution UPDATE and the second poll
// issues none at all, not even one that the IS DISTINCT FROM guard would turn
// into a no-op.
func TestQueueWaitDBSteadyStateNoRewrite(t *testing.T) {
	f := newClaimDecisionFixture(t, 0)
	taskID := f.insertTask(t, "queued")

	tracer := &queueWaitUpdateTracer{}
	pool := tracedQueueWaitPool(t, tracer)
	svc := NewTaskService(db.New(pool), pool, nil, events.New())

	ctx := context.Background()
	claimed, err := svc.ClaimTaskForRuntime(ctx, f.runtimeID)
	if err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if claimed != nil {
		t.Fatal("first poll claimed a task at max_concurrent_tasks=0")
	}
	if tracer.updates != 1 {
		t.Fatalf("first poll issued %d UpdateQueueWaitReasons statement(s), want 1", tracer.updates)
	}
	wantReason(t, f.pool, taskID, queueWaitAgentCapacity)

	claimed, err = svc.ClaimTaskForRuntime(ctx, f.runtimeID)
	if err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if claimed != nil {
		t.Fatal("second poll claimed a task at max_concurrent_tasks=0")
	}
	// The second poll read the same value back through the candidate SELECT
	// and must not have issued the UPDATE again.
	if tracer.updates != 1 {
		t.Fatalf("second poll issued %d UpdateQueueWaitReasons statement(s), want 0", tracer.updates-1)
	}
	wantReason(t, f.pool, taskID, queueWaitAgentCapacity)
}

// TestQueueWaitDBBatchClaimAttribution is the batch-path twin of the singular
// lifecycle above: with max_concurrent_tasks=1 and two queued tasks on one
// runtime, the first ClaimTasksForRuntimes claims the head (clearing any
// attribution it carried) and the second attributes the still-queued tail
// agent_capacity. Pins that the batch loop feeds its attempts into
// recordQueueWaitReasons and that the dispatched head stays out of the write —
// the candidate SELECT's status='queued' filter excludes it, and the UPDATE's
// own fence would reject it even if a stale row were listed.
func TestQueueWaitDBBatchClaimAttribution(t *testing.T) {
	f := newClaimDecisionFixture(t, 1)
	otherIssue := f.fx.Issue(t, "queue wait batch second issue")
	head := f.insertTask(t, "queued")
	tail := f.fx.Task(t, util.UUIDToString(f.agentID), testutil.Cols{
		"runtime_id": util.UUIDToString(f.runtimeID),
		"issue_id":   otherIssue,
		"status":     "queued",
	})

	// Simulate an attribution an earlier poll wrote for the head, so the
	// claim-clear is observable on the very first batch claim.
	if _, err := f.pool.Exec(context.Background(),
		`UPDATE agent_task_queue SET queue_wait_reason = 'attempted_unclaimed' WHERE id = $1`, head,
	); err != nil {
		t.Fatalf("seed head attribution: %v", err)
	}

	ctx := context.Background()
	first, err := f.svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{f.runtimeID}, 2)
	if err != nil {
		t.Fatalf("first batch claim: %v", err)
	}
	if len(first) != 1 || util.UUIDToString(first[0].ID) != head {
		t.Fatalf("first batch claim returned %v, want head %s", first, head)
	}
	// Leaving the queue cleared the head's attribution; the tail was never
	// attempted by this batch (one attempt per agent), so it carries none yet.
	wantReason(t, f.pool, head, "")
	wantReason(t, f.pool, tail, "")

	second, err := f.svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{f.runtimeID}, 2)
	if err != nil {
		t.Fatalf("second batch claim: %v", err)
	}
	if len(second) != 0 {
		t.Fatalf("second batch claim returned %v, want none", second)
	}
	// The dispatched head occupies the agent's only slot: the batch poll
	// attributes the tail agent_capacity and never touches the head.
	wantReason(t, f.pool, tail, queueWaitAgentCapacity)
	wantReason(t, f.pool, head, "")
}

// TestQueueWaitDBWriteFailureNeverBreaksClaim pins the nil path: when the
// attribution write fails, the failure is swallowed and the claim still works.
// The service is wired with a Queries handle on an unreachable pool while the
// transaction starter (and therefore every claim query) stays on the healthy
// pool — claimTask's transactional writes ride WithTx, while the
// post-loop attribution write goes through the broken handle.
func TestQueueWaitDBWriteFailureNeverBreaksClaim(t *testing.T) {
	f := newClaimDecisionFixture(t, 5)
	taskID := f.insertTask(t, "queued")

	broken, err := pgxpool.New(context.Background(), "host=127.0.0.1 port=1 connect_timeout=1")
	if err != nil {
		t.Skipf("cannot build unreachable pool: %v", err)
	}
	t.Cleanup(broken.Close)
	svc := NewTaskService(db.New(broken), f.pool, nil, events.New())

	// Candidate rows from the healthy pool; the attribution write goes through
	// the broken handle and must be swallowed, not propagated, and must not
	// touch the row.
	candidates, aerr := f.svc.Queries.ListQueuedClaimCandidatesByRuntime(context.Background(), f.runtimeID)
	if aerr != nil {
		t.Fatalf("list candidates: %v", aerr)
	}
	agentKey := util.UUIDToString(f.agentID)
	svc.recordQueueWaitReasons(context.Background(),
		map[string][]db.AgentTaskQueue{agentKey: candidates},
		map[string]claimAttempt{agentKey: {Outcome: claimOutcomeNoCapacity}},
	)
	wantReason(t, f.pool, taskID, "")

	// The claim itself must still succeed on the healthy pool.
	claimed, _, cerr := svc.claimTask(context.Background(), f.agentID, f.runtimeID, claimScope{claimPath: claimPathRuntime})
	err = cerr
	if err != nil {
		t.Fatalf("claim with broken attribution handle: %v", err)
	}
	if claimed == nil || util.UUIDToString(claimed.ID) != taskID {
		t.Fatalf("claim with broken attribution handle returned %v, want %s", claimed, taskID)
	}
}
