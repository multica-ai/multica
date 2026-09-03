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
	got, err := svc.ClaimTasksForRuntimes(ctx, ids, 5)
	if err != nil {
		t.Fatalf("unforced claim: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("unforced claim returned %d tasks, want 0 (stale empty verdict must short-circuit)", len(got))
	}

	// Force only rt1: it bypasses the short-circuit and claims its queued task,
	// while rt2 (not forced) stays short-circuited.
	got, err = svc.ClaimTasksForRuntimes(ctx, ids, 5, util.MustParseUUID(rt1))
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
	if got, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{rt2ID}, 5); err != nil {
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
	got, err := svc.ClaimTasksForRuntimes(ctx, []pgtype.UUID{rt2ID}, 5, rt2ID)
	if err != nil {
		t.Fatalf("forced idle claim: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("forced idle claim returned %d tasks, want 0", len(got))
	}
	if !svc.EmptyClaim.IsEmpty(ctx, rt2) {
		t.Fatal("forced re-check on a zero-candidate runtime must re-arm the empty verdict")
	}
}
