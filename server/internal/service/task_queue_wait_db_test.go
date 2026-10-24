package service

import (
	"context"
	"testing"

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

// TestQueueWaitDBSteadyStateNoRewrite covers the steady state: a second poll
// with the same outcome must not rewrite the row. The zero-write guarantee
// itself is pinned by the pure test (a row already carrying the reason yields
// no update); here the behavior is asserted end to end — the value survives a
// repeated poll unchanged and the poll keeps returning cleanly.
func TestQueueWaitDBSteadyStateNoRewrite(t *testing.T) {
	f := newClaimDecisionFixture(t, 0)
	taskID := f.insertTask(t, "queued")

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		claimed, err := f.svc.ClaimTaskForRuntime(ctx, f.runtimeID)
		if err != nil {
			t.Fatalf("poll %d: %v", i+1, err)
		}
		if claimed != nil {
			t.Fatalf("poll %d claimed a task at max_concurrent_tasks=0", i+1)
		}
	}
	// First poll wrote agent_capacity; the second poll read the same value
	// back through the candidate SELECT and issued no update for it.
	wantReason(t, f.pool, taskID, queueWaitAgentCapacity)
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
