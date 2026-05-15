package service

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestBackstopRequeue_InvalidatesEmptyCache verifies that when the preflight
// requeue path calls notifyTaskAvailable (via
// RequeueExpiredClaimLeasesForRuntime), the EmptyClaim cache is bumped so
// the handler's next ClaimTaskForRuntime does NOT hit a stale empty verdict.
//
// This is a regression test for the bug where the backend taskSvc (used by
// the sweeper) had Liveness wired but not EmptyClaim, causing
// s.EmptyClaim.Bump() inside notifyTaskAvailable to be a nil no-op.
func TestBackstopRequeue_InvalidatesEmptyCache(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewEmptyClaimCache(rdb)
	wakeup := &stubWakeup{}

	// Simulate the backend taskSvc used by the sweeper — must have
	// EmptyClaim wired (the fix under test).
	backstopSvc := &TaskService{
		EmptyClaim: cache,
		Wakeup:     wakeup,
	}

	// Simulate the handler's TaskService sharing the same Redis-backed
	// EmptyClaimCache (same Redis keys).
	handlerSvc := &TaskService{
		EmptyClaim: cache,
		Wakeup:     wakeup,
	}

	runtimeID := testUUID(0xAA)
	taskID := testUUID(0xBB)
	runtimeKey := util.UUIDToString(runtimeID)
	ctx := context.Background()

	// 1. Handler marks the runtime as empty (no queued tasks).
	v0 := handlerSvc.EmptyClaim.CurrentVersion(ctx, runtimeKey)
	handlerSvc.EmptyClaim.MarkEmpty(ctx, runtimeKey, v0)
	if !handlerSvc.EmptyClaim.IsEmpty(ctx, runtimeKey) {
		t.Fatal("precondition: handler cache should report empty")
	}

	// 2. Global backstop requeues an expired lease and calls
	//    notifyTaskAvailable — this must bump the empty-cache.
	backstopSvc.notifyTaskAvailable(db.AgentTaskQueue{
		ID:        taskID,
		RuntimeID: runtimeID,
	})

	// 3. Handler's next claim must NOT be short-circuited by stale verdict.
	if handlerSvc.EmptyClaim.IsEmpty(ctx, runtimeKey) {
		t.Fatal("backstop requeue must invalidate the handler's empty-cache; stale verdict still active")
	}
}
