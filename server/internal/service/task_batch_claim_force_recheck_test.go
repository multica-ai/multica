package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestClaimTasksForRuntimes_ForceRecheckBypassesStaleEmptyVerdict is the #7452
// regression: a runtime with a queued task but a stale cached "empty" verdict
// (as if the enqueue's EmptyClaim.Bump was lost to a transient Redis failure)
// is stranded on a normal claim, yet a claim that names it in the force set
// bypasses the short-circuit and claims the task immediately. A second runtime
// that is NOT forced still short-circuits, proving the bypass is targeted.
func TestClaimTasksForRuntimes_ForceRecheckBypassesStaleEmptyVerdict(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	rdb := newRedisTestClient(t)

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	svc.EmptyClaim = NewEmptyClaimCache(rdb)

	rt1, rt2 := batchClaimFixture(t, ctx, pool)
	rt1Key, rt2Key := rt1, rt2
	ids := []pgtype.UUID{util.MustParseUUID(rt1), util.MustParseUUID(rt2)}

	// Simulate the missed-invalidation state on BOTH runtimes: a verdict tagged
	// with the current version, so IsEmpty trusts it even though rows are queued.
	svc.EmptyClaim.MarkEmpty(ctx, rt1Key, svc.EmptyClaim.CurrentVersion(ctx, rt1Key))
	svc.EmptyClaim.MarkEmpty(ctx, rt2Key, svc.EmptyClaim.CurrentVersion(ctx, rt2Key))
	if !svc.EmptyClaim.IsEmpty(ctx, rt1Key) || !svc.EmptyClaim.IsEmpty(ctx, rt2Key) {
		t.Fatal("precondition: both runtimes must read as cached-empty")
	}

	// No force set: both runtimes short-circuit on the stale verdict → nothing
	// is claimed even though tasks are queued.
	got, _, err := svc.ClaimTasksForRuntimes(ctx, ids, 5)
	if err != nil {
		t.Fatalf("unforced claim: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("unforced claim returned %d tasks, want 0 (stale empty verdict must short-circuit)", len(got))
	}

	// Force only rt1: it bypasses the short-circuit and claims its queued task,
	// while rt2 (not forced) stays short-circuited.
	got, _, err = svc.ClaimTasksForRuntimes(ctx, ids, 5, util.MustParseUUID(rt1))
	if err != nil {
		t.Fatalf("forced claim: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("forced claim returned %d tasks, want exactly 1 (only rt1 bypassed)", len(got))
	}
	if util.UUIDToString(got[0].RuntimeID) != rt1 {
		t.Fatalf("forced claim routed to %s, want rt1", util.UUIDToString(got[0].RuntimeID))
	}
}

// TestClaimTasksForRuntimes_ForceRecheckReArmsCacheOnZeroCandidates pins the
// other half of #7452: forcing a re-check on a genuinely-idle runtime must not
// leave the cache disabled — a zero-candidate result re-arms the empty verdict
// (MarkEmpty), so subsequent idle polls skip Postgres again.
func TestClaimTasksForRuntimes_ForceRecheckReArmsCacheOnZeroCandidates(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	rdb := newRedisTestClient(t)

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	svc.EmptyClaim = NewEmptyClaimCache(rdb)

	_, rt2 := batchClaimFixture(t, ctx, pool)
	rt2ID := util.MustParseUUID(rt2)

	// Drain rt2's single queued task so it becomes genuinely idle in the DB.
	if got, _, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{rt2ID}, 5); err != nil {
		t.Fatalf("drain claim: %v", err)
	} else if len(got) != 1 {
		t.Fatalf("drain claim returned %d tasks, want 1", len(got))
	}

	// Invalidate any cached verdict so IsEmpty is false going into the forced call.
	svc.EmptyClaim.Bump(ctx, rt2)
	if svc.EmptyClaim.IsEmpty(ctx, rt2) {
		t.Fatal("precondition: rt2 must not read as cached-empty after Bump")
	}

	// Force a re-check on the now-idle runtime: nothing to claim, but the empty
	// verdict must be re-armed for the next idle poll.
	got, acknowledged, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{rt2ID}, 5, rt2ID)
	if err != nil {
		t.Fatalf("forced idle claim: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("forced idle claim returned %d tasks, want 0", len(got))
	}
	if !svc.EmptyClaim.IsEmpty(ctx, rt2) {
		t.Fatal("forced re-check on a zero-candidate runtime must re-arm the empty verdict")
	}
	// A zero-candidate scan is the ONLY thing that acknowledges a forced runtime
	// (#7452): the runtime is confirmed idle and its cache entry was just re-armed
	// from a real SELECT, so the daemon may drop the hint.
	if len(acknowledged) != 1 || acknowledged[0] != rt2 {
		t.Fatalf("acknowledged set = %v, want [rt2] after a zero-candidate forced scan", acknowledged)
	}
}

// TestClaimTasksForRuntimes_ForcedPositiveIsNotAcknowledged pins refinement 1 of
// #7452: a forced runtime whose SELECT returns candidates is NOT acknowledged,
// so the daemon keeps it forced and re-forces it next cycle. The batch claims at
// most one task per agent per call, so a forced claim on a runtime with TWO
// queued tasks for the same agent takes only the first and leaves the second
// queued behind a stale "empty" verdict. Acknowledging here would have made
// correctness depend on a Redis repair write landing — and that write can fail in
// the very outage that lost the enqueue-side Bump. Staying forced until a scan
// comes back empty needs no such write: the next forced claim takes the second
// task immediately, without waiting for the ~3 minute TTL.
func TestClaimTasksForRuntimes_ForcedPositiveIsNotAcknowledged(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	rdb := newRedisTestClient(t)

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	svc.EmptyClaim = NewEmptyClaimCache(rdb)

	// batchClaimFixture gives rt1 an agent with TWO queued tasks on two issues.
	rt1, _ := batchClaimFixture(t, ctx, pool)
	rt1ID := util.MustParseUUID(rt1)

	// Simulate the lost-Bump state: a verdict tagged with the current version, so
	// IsEmpty trusts it even though two tasks are queued on rt1.
	svc.EmptyClaim.MarkEmpty(ctx, rt1, svc.EmptyClaim.CurrentVersion(ctx, rt1))
	if !svc.EmptyClaim.IsEmpty(ctx, rt1) {
		t.Fatal("precondition: rt1 must read as cached-empty")
	}

	// Forced claim: bypasses the short-circuit and claims exactly one of the two
	// same-agent tasks. Because the scan found candidates, rt1 is NOT acknowledged.
	forced, acknowledged, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{rt1ID}, 5, rt1ID)
	if err != nil {
		t.Fatalf("forced claim: %v", err)
	}
	if len(forced) != 1 || util.UUIDToString(forced[0].RuntimeID) != rt1 {
		t.Fatalf("forced claim = %d tasks on %v, want exactly 1 on rt1", len(forced), forced)
	}
	if len(acknowledged) != 0 {
		t.Fatalf("acknowledged set = %v, want empty: a forced scan that found candidates must stay forced", acknowledged)
	}

	// A NORMAL poll still short-circuits on the stale verdict, which is exactly
	// why the runtime must stay forced rather than be acknowledged here.
	if stranded, _, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{rt1ID}, 5); err != nil {
		t.Fatalf("normal follow-up claim: %v", err)
	} else if len(stranded) != 0 {
		t.Fatalf("normal follow-up claim = %d tasks, want 0 (the stale verdict still short-circuits)", len(stranded))
	}

	// The daemon kept rt1 forced, so the next FORCED claim takes the second
	// same-agent task instead of stranding it until the TTL.
	second, secondAck, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{rt1ID}, 5, rt1ID)
	if err != nil {
		t.Fatalf("second forced claim: %v", err)
	}
	if len(second) != 1 || util.UUIDToString(second[0].RuntimeID) != rt1 {
		t.Fatalf("second forced claim = %d tasks on %v, want the second task on rt1", len(second), second)
	}
	if forced[0].ID == second[0].ID {
		t.Fatal("second forced claim returned the same task, want the distinct second same-agent task")
	}
	// The agent's only remaining candidate was attempted and claimed, so nothing
	// claimable is left behind and rt1 is settled: the daemon may drop the hint.
	if len(secondAck) != 1 || secondAck[0] != rt1 {
		t.Fatalf("acknowledged set = %v, want [rt1]: the last candidate was claimed", secondAck)
	}

	// A further forced scan on the drained queue comes back empty and also
	// acknowledges rt1.
	drained, drainedAck, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{rt1ID}, 5, rt1ID)
	if err != nil {
		t.Fatalf("drained forced claim: %v", err)
	}
	if len(drained) != 0 {
		t.Fatalf("drained forced claim = %d tasks, want 0", len(drained))
	}
	if len(drainedAck) != 1 || drainedAck[0] != rt1 {
		t.Fatalf("acknowledged set = %v, want [rt1] once the forced scan is empty", drainedAck)
	}
}

// TestClaimTasksForRuntimes_ForcedBlockedCandidateIsAcknowledged: a forced
// runtime whose only queued task cannot be claimed right now (the same agent
// already runs a task on that issue) is acknowledged. Every candidate agent was
// attempted and nothing claimable was left behind, and the blocker's terminal
// transition emits its own Bump plus targeted wakeup. Leaving it unacknowledged
// would hold the daemon on the short poll for as long as the blocker runs.
func TestClaimTasksForRuntimes_ForcedBlockedCandidateIsAcknowledged(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	rdb := newRedisTestClient(t)

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	svc.EmptyClaim = NewEmptyClaimCache(rdb)

	_, rt2 := batchClaimFixture(t, ctx, pool)
	rt2ID := util.MustParseUUID(rt2)
	if _, err := pool.Exec(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, status, priority, context, runtime_id, dispatched_at, started_at)
		SELECT agent_id, issue_id, 'running', 0, '{}'::jsonb, runtime_id, now(), now()
		FROM agent_task_queue WHERE runtime_id = $1 AND status = 'queued'`, rt2); err != nil {
		t.Fatalf("insert running same-issue sibling: %v", err)
	}

	got, acknowledged, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{rt2ID}, 5, rt2ID)
	if err != nil {
		t.Fatalf("forced claim: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("forced claim returned %d tasks, want 0 (the only candidate is serialized behind a running task)", len(got))
	}
	if len(acknowledged) != 1 || acknowledged[0] != rt2 {
		t.Fatalf("acknowledged set = %v, want [rt2]: a blocked candidate must not keep the runtime forced", acknowledged)
	}
}

// TestClaimTasksForRuntimes_ForcedRuntimeCutOffByMaxTasksStaysForced: when
// max_tasks stops the claim loop before a forced runtime's agent is attempted,
// that runtime may still hold a claimable task, so it is not acknowledged.
func TestClaimTasksForRuntimes_ForcedRuntimeCutOffByMaxTasksStaysForced(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	rdb := newRedisTestClient(t)

	svc := NewTaskService(db.New(pool), pool, nil, events.New())
	svc.EmptyClaim = NewEmptyClaimCache(rdb)

	rt1, rt2 := batchClaimFixture(t, ctx, pool)
	rt1ID, rt2ID := util.MustParseUUID(rt1), util.MustParseUUID(rt2)

	// Candidates are ordered FIFO: rt1's two tasks precede rt2's, so with
	// max_tasks=1 the loop claims rt1's first task and never attempts rt2.
	got, acknowledged, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{rt1ID, rt2ID}, 1, rt1ID, rt2ID)
	if err != nil {
		t.Fatalf("forced claim: %v", err)
	}
	if len(got) != 1 || util.UUIDToString(got[0].RuntimeID) != rt1 {
		t.Fatalf("forced claim = %v, want exactly one task on rt1", got)
	}
	if len(acknowledged) != 0 {
		t.Fatalf("acknowledged set = %v, want empty: rt1 has a second same-agent task and rt2 was never attempted", acknowledged)
	}
}
