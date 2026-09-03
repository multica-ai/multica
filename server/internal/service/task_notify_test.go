package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestTaskClaimableWaitSecondsExcludesDeferredDelay(t *testing.T) {
	createdAt := time.Unix(1_000, 0)
	fireAt := createdAt.Add(10 * time.Minute)
	dispatchedAt := fireAt.Add(3 * time.Second)
	task := db.AgentTaskQueue{
		CreatedAt:    pgtype.Timestamptz{Time: createdAt, Valid: true},
		FireAt:       pgtype.Timestamptz{Time: fireAt, Valid: true},
		DispatchedAt: pgtype.Timestamptz{Time: dispatchedAt, Valid: true},
	}

	if got := taskClaimableWaitSeconds(task); got != 3 {
		t.Fatalf("taskClaimableWaitSeconds() = %v, want 3", got)
	}
	task.FireAt = pgtype.Timestamptz{}
	if got := taskClaimableWaitSeconds(task); got != 603 {
		t.Fatalf("immediate task claimable wait = %v, want 603", got)
	}
}

func TestEnqueueDeferredAssigneeFallbackRefreshesRuntimeSchedule(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	fx := testutil.New(pool, workspaceID, userID)
	triggerCommentID := fx.Comment(t, issueID, "fallback trigger")
	agent, err := q.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	primaryTaskID := fx.Task(t, agentID, testutil.Cols{
		"runtime_id":         util.UUIDToString(agent.RuntimeID),
		"issue_id":           issueID,
		"status":             "running",
		"trigger_comment_id": triggerCommentID,
	})
	issue, err := q.GetIssue(ctx, util.MustParseUUID(issueID))
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	wakeup := &stubWakeup{}
	svc := &TaskService{Queries: q, TxStarter: pool, Wakeup: wakeup}

	task, err := svc.EnqueueDeferredAssigneeFallback(ctx, issue, util.MustParseUUID(agentID), pgtype.UUID{}, util.MustParseUUID(primaryTaskID), util.MustParseUUID(triggerCommentID), time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("EnqueueDeferredAssigneeFallback: %v", err)
	}
	fx.Cleanup(t, `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
	if task.Status != "deferred" || len(wakeup.calls) != 1 {
		t.Fatalf("fallback task/status wakeups = %q/%+v, want deferred with one schedule refresh", task.Status, wakeup.calls)
	}
	if wakeup.calls[0].runtimeID != util.UUIDToString(task.RuntimeID) || wakeup.calls[0].taskID != "" {
		t.Fatalf("fallback schedule wakeup = %+v, want runtime %s without task id", wakeup.calls[0], util.UUIDToString(task.RuntimeID))
	}
}

// stubWakeup records every call so the test can assert that notify
// reaches the daemon hub and carries the right runtime / task IDs.
type stubWakeup struct {
	calls []struct{ runtimeID, taskID string }
}

func (s *stubWakeup) NotifyTaskAvailable(runtimeID, taskID string) {
	s.calls = append(s.calls, struct{ runtimeID, taskID string }{runtimeID, taskID})
}

// TestNotifyTaskAvailable_BumpsBeforeWakeup pins the contract noted in
// the EmptyClaimCache docs: the version Bump MUST run before the
// daemon WS wakeup, otherwise the wakeup-driven claim could read a
// still-current empty verdict and return null while the freshly
// queued task sits idle. The test (1) marks the runtime empty under
// the current version, (2) fires notifyTaskAvailable, then (3)
// asserts the prior verdict is rejected AND the wakeup hook saw the
// new task — proving every enqueue path (issue / mention /
// quick-create / chat / autopilot / retry) gets the same
// bump-then-notify behaviour for free.
func TestNotifyTaskAvailable_BumpsBeforeWakeup(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewEmptyClaimCache(rdb)
	wakeup := &stubWakeup{}

	svc := &TaskService{
		EmptyClaim: cache,
		Wakeup:     wakeup,
	}

	runtimeID := testUUID(7)
	taskID := testUUID(8)
	runtimeKey := util.UUIDToString(runtimeID)

	ctx := context.Background()
	v0 := cache.CurrentVersion(ctx, runtimeKey)
	cache.MarkEmpty(ctx, runtimeKey, v0)
	if !cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("precondition: cache should report empty after MarkEmpty under current version")
	}

	svc.notifyTaskAvailable(db.AgentTaskQueue{
		ID:        taskID,
		RuntimeID: runtimeID,
	})

	if cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("notifyTaskAvailable must Bump the version so the prior empty verdict is rejected")
	}
	if got := len(wakeup.calls); got != 1 {
		t.Fatalf("expected 1 wakeup call, got %d", got)
	}
	if wakeup.calls[0].runtimeID != runtimeKey {
		t.Fatalf("wakeup runtime mismatch: got %q want %q", wakeup.calls[0].runtimeID, runtimeKey)
	}
	if wakeup.calls[0].taskID != util.UUIDToString(taskID) {
		t.Fatalf("wakeup task mismatch: got %q want %q", wakeup.calls[0].taskID, util.UUIDToString(taskID))
	}
}

// TestNotifyTaskAvailable_InvalidWithoutRuntimeIsNoOp guards the
// no-RuntimeID early return — chat / quick-create / autopilot all set
// it on insert, but a buggy caller that forgot must not silently bump
// every workspace's version. The cache treats Bump("") as a no-op,
// but this test pins that the RuntimeID guard sits above the Bump
// call so a future refactor cannot drop the guard without test
// coverage.
func TestNotifyTaskAvailable_InvalidWithoutRuntimeIsNoOp(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewEmptyClaimCache(rdb)
	wakeup := &stubWakeup{}

	svc := &TaskService{
		EmptyClaim: cache,
		Wakeup:     wakeup,
	}

	ctx := context.Background()
	v0 := cache.CurrentVersion(ctx, "rt-stays")
	cache.MarkEmpty(ctx, "rt-stays", v0)

	svc.notifyTaskAvailable(db.AgentTaskQueue{
		// RuntimeID intentionally invalid (zero value, Valid=false).
		ID: testUUID(9),
	})

	if !cache.IsEmpty(ctx, "rt-stays") {
		t.Fatal("notifyTaskAvailable with invalid RuntimeID must not touch cache")
	}
	if got := len(wakeup.calls); got != 0 {
		t.Fatalf("expected 0 wakeup calls when RuntimeID is invalid, got %d", got)
	}
}

// TestNotifyTaskFinished_BumpsBeforeRuntimeWakeup pins the terminal-transition
// half of the wakeup contract. A prior empty verdict must not hide queued work
// that becomes claimable when another task releases agent capacity or a
// per-(issue, agent) serialization key.
func TestNotifyTaskFinished_BumpsBeforeRuntimeWakeup(t *testing.T) {
	rdb := newRedisTestClient(t)
	cache := NewEmptyClaimCache(rdb)
	wakeup := &stubWakeup{}
	svc := &TaskService{EmptyClaim: cache, Wakeup: wakeup}

	runtimeID := testUUID(10)
	runtimeKey := util.UUIDToString(runtimeID)
	ctx := context.Background()
	version := cache.CurrentVersion(ctx, runtimeKey)
	cache.MarkEmpty(ctx, runtimeKey, version)
	if !cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("precondition: cache should report empty")
	}

	svc.NotifyTaskFinished(db.AgentTaskQueue{ID: testUUID(11), RuntimeID: runtimeID})

	if cache.IsEmpty(ctx, runtimeKey) {
		t.Fatal("terminal wakeup must invalidate the prior empty verdict")
	}
	if got := len(wakeup.calls); got != 1 {
		t.Fatalf("expected 1 terminal wakeup, got %d", got)
	}
	if wakeup.calls[0].runtimeID != runtimeKey {
		t.Fatalf("wakeup runtime mismatch: got %q want %q", wakeup.calls[0].runtimeID, runtimeKey)
	}
	if wakeup.calls[0].taskID != "" {
		t.Fatalf("terminal wakeup must omit completed task id, got %q", wakeup.calls[0].taskID)
	}
}

func TestNotifyTasksFinished_CoalescesByRuntime(t *testing.T) {
	wakeup := &stubWakeup{}
	svc := &TaskService{Wakeup: wakeup}
	runtimeA := testUUID(12)
	runtimeB := testUUID(13)

	svc.notifyTasksFinished([]db.AgentTaskQueue{
		{ID: testUUID(14), RuntimeID: runtimeA},
		{ID: testUUID(15), RuntimeID: runtimeA},
		{ID: testUUID(16), RuntimeID: runtimeB},
		{ID: testUUID(17)}, // Invalid runtime is ignored.
	})

	if got := len(wakeup.calls); got != 2 {
		t.Fatalf("expected one wakeup per runtime, got %d", got)
	}
	if wakeup.calls[0].runtimeID != util.UUIDToString(runtimeA) || wakeup.calls[1].runtimeID != util.UUIDToString(runtimeB) {
		t.Fatalf("unexpected runtime wakeups: %#v", wakeup.calls)
	}
	for _, call := range wakeup.calls {
		if call.taskID != "" {
			t.Fatalf("terminal wakeup must omit completed task id, got %q", call.taskID)
		}
	}
}

// TestNotifyTaskEnqueued_UnwiredServiceCannotInvalidateClaimVerdict documents
// why enqueue and claim must run through the SAME TaskService instance.
//
// EmptyClaimCache is nil-safe by design so a deployment without Redis degrades
// to direct DB lookups. The cost of that convenience is that a TaskService
// which never had EmptyClaim assigned fails silently instead of loudly: Bump
// is a no-op while the daemon wakeup still fires. Pair that with a second,
// fully wired TaskService serving claims and the runtime is woken for a task
// it is then told does not exist — for up to EmptyClaimCacheTTL.
//
// This is the failure the process wiring in cmd/server avoids by handing
// background workers the router's service (see
// TestMainUsesRouterOwnedBackgroundServices). Asserting it here keeps the
// reason legible from the cache's own package: if a future change makes the
// unwired case impossible — e.g. EmptyClaim becomes a constructor argument —
// this test should be deleted along with the hazard, not adjusted to fit.
func TestNotifyTaskEnqueued_UnwiredServiceCannotInvalidateClaimVerdict(t *testing.T) {
	rdb := newRedisTestClient(t)
	ctx := context.Background()

	// The router's service: serves daemon claims, owns the cache.
	claimSvc := &TaskService{EmptyClaim: NewEmptyClaimCache(rdb), Wakeup: &stubWakeup{}}
	// A background service built without the router's post-construction
	// wiring. Same wakeup notifier, no EmptyClaim.
	enqueueWakeup := &stubWakeup{}
	unwiredSvc := &TaskService{Wakeup: enqueueWakeup}

	runtimeID := testUUID(18)
	runtimeKey := util.UUIDToString(runtimeID)

	// An idle runtime polled, found nothing, and cached that verdict.
	claimSvc.EmptyClaim.MarkEmpty(ctx, runtimeKey, claimSvc.EmptyClaim.CurrentVersion(ctx, runtimeKey))
	if !claimSvc.EmptyClaim.IsEmpty(ctx, runtimeKey) {
		t.Fatal("precondition: claim path should see a cached empty verdict")
	}

	unwiredSvc.NotifyTaskEnqueued(ctx, db.AgentTaskQueue{ID: testUUID(19), RuntimeID: runtimeID})

	if got := len(enqueueWakeup.calls); got != 1 {
		t.Fatalf("expected the daemon wakeup to fire regardless of cache wiring, got %d calls", got)
	}
	// ClaimTaskForRuntime consults exactly this predicate before querying
	// Postgres, so a surviving verdict means the woken claim returns nil.
	if !claimSvc.EmptyClaim.IsEmpty(ctx, runtimeKey) {
		t.Fatal("unexpected: an EmptyClaim-less service invalidated the verdict — the split-instance hazard this documents is gone, so delete this test")
	}

	// Same enqueue through the service that owns the cache: verdict dies.
	claimSvc.NotifyTaskEnqueued(ctx, db.AgentTaskQueue{ID: testUUID(20), RuntimeID: runtimeID})
	if claimSvc.EmptyClaim.IsEmpty(ctx, runtimeKey) {
		t.Fatal("enqueue through the claim path's own service must invalidate the verdict")
	}
}
