package seatcapacity

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type workerTestExecutor struct {
	decision          Decision
	err               error
	errorsByWorkspace map[uuid.UUID]error
	confirmWorkspaces []uuid.UUID
}

func (*workerTestExecutor) RecoveryAvailable() bool { return true }

type workerTestLocker struct {
	locks   int
	unlocks int
}

func (l *workerTestLocker) Lock(context.Context, uuid.UUID) (db.DBTX, func(), error) {
	l.locks++
	return nil, func() { l.unlocks++ }, nil
}

func (e *workerTestExecutor) ReserveInvitation(context.Context, uuid.UUID, uuid.UUID, time.Time) (Decision, error) {
	return Decision{}, nil
}
func (e *workerTestExecutor) ClaimShareJoin(context.Context, uuid.UUID, uuid.UUID) (Decision, error) {
	return Decision{}, nil
}
func (e *workerTestExecutor) Consume(context.Context, uuid.UUID, uuid.UUID) (Decision, error) {
	return Decision{}, nil
}

func (e *workerTestExecutor) Confirm(_ context.Context, workspaceID, _, _ uuid.UUID) (Decision, error) {
	e.confirmWorkspaces = append(e.confirmWorkspaces, workspaceID)
	if err := e.errorsByWorkspace[workspaceID]; err != nil {
		return Decision{}, err
	}
	return e.decision, e.err
}
func (e *workerTestExecutor) Release(context.Context, uuid.UUID, uuid.UUID) (Decision, error) {
	return e.decision, e.err
}
func (e *workerTestExecutor) ReleaseMember(context.Context, uuid.UUID, uuid.UUID) (Decision, error) {
	return e.decision, e.err
}
func (e *workerTestExecutor) GetOperation(context.Context, uuid.UUID, uuid.UUID) (Decision, error) {
	return e.decision, e.err
}

type workerTestQueries struct {
	mu sync.Mutex

	intent          db.SeatCapacityOutbox
	intents         []db.SeatCapacityOutbox
	nextIntent      int
	claimAvailable  bool
	invitation      db.WorkspaceInvitation
	invitationError error
	stats           []db.SeatCapacityOutboxStatsRow
	deferredUntil   pgtype.Timestamptz
	deferredUntils  []pgtype.Timestamptz

	claimCalls  int
	transitions int
	deletes     int
	expires     int
	failures    int
	deadLetters int
	deferrals   int
}

func (q *workerTestQueries) ClaimNextDueSeatCapacityIntent(context.Context, pgtype.Timestamptz) (db.SeatCapacityOutbox, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claimCalls++
	if len(q.intents) > 0 {
		if q.nextIntent >= len(q.intents) {
			return db.SeatCapacityOutbox{}, pgx.ErrNoRows
		}
		q.intent = q.intents[q.nextIntent]
		q.nextIntent++
		q.intent.LeaseToken = uuidToTestPG(uuid.New())
		return q.intent, nil
	}
	if !q.claimAvailable {
		return db.SeatCapacityOutbox{}, pgx.ErrNoRows
	}
	q.claimAvailable = false
	q.intent.LeaseToken = uuidToTestPG(uuid.New())
	return q.intent, nil
}

func (q *workerTestQueries) DeferClaimedSeatCapacityIntent(_ context.Context, arg db.DeferClaimedSeatCapacityIntentParams) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.intent.OperationToken != arg.OperationToken || q.intent.Action != arg.Action || q.intent.LeaseToken != arg.LeaseToken {
		return 0, nil
	}
	q.deferrals++
	q.deferredUntil = arg.NextAttemptAt
	q.deferredUntils = append(q.deferredUntils, arg.NextAttemptAt)
	q.intent.LeaseToken = pgtype.UUID{}
	return 1, nil
}

func (q *workerTestQueries) DeleteClaimedSeatCapacityIntent(_ context.Context, arg db.DeleteClaimedSeatCapacityIntentParams) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.intent.OperationToken == arg.OperationToken && q.intent.Action == arg.Action && q.intent.LeaseToken == arg.LeaseToken {
		q.deletes++
		return 1, nil
	}
	return 0, nil
}

func (q *workerTestQueries) ExpireInvitationForCapacityRecovery(context.Context, pgtype.UUID) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.expires++
	return nil
}

func (q *workerTestQueries) GetInvitation(context.Context, pgtype.UUID) (db.WorkspaceInvitation, error) {
	return q.invitation, q.invitationError
}

func (q *workerTestQueries) GetClaimedSeatCapacityIntent(_ context.Context, arg db.GetClaimedSeatCapacityIntentParams) (db.SeatCapacityOutbox, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.intent.OperationToken != arg.OperationToken || q.intent.Action != arg.Action || q.intent.LeaseToken != arg.LeaseToken {
		return db.SeatCapacityOutbox{}, pgx.ErrNoRows
	}
	return q.intent, nil
}

func (q *workerTestQueries) MarkClaimedSeatCapacityIntentDeadLettered(_ context.Context, arg db.MarkClaimedSeatCapacityIntentDeadLetteredParams) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.intent.OperationToken != arg.OperationToken || q.intent.Action != arg.Action || q.intent.LeaseToken != arg.LeaseToken {
		return 0, nil
	}
	q.deadLetters++
	q.intent.LeaseToken = pgtype.UUID{}
	return 1, nil
}

func (q *workerTestQueries) MarkClaimedSeatCapacityIntentFailed(_ context.Context, arg db.MarkClaimedSeatCapacityIntentFailedParams) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.intent.OperationToken != arg.OperationToken || q.intent.Action != arg.Action || q.intent.LeaseToken != arg.LeaseToken {
		return 0, nil
	}
	q.failures++
	q.intent.LeaseToken = pgtype.UUID{}
	return 1, nil
}

func (q *workerTestQueries) SeatCapacityOutboxStats(context.Context) ([]db.SeatCapacityOutboxStatsRow, error) {
	return q.stats, nil
}

func (q *workerTestQueries) TransitionClaimedSeatCapacityIntent(_ context.Context, arg db.TransitionClaimedSeatCapacityIntentParams) (int64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.intent.OperationToken != arg.OperationToken || q.intent.Action != arg.CurrentAction || q.intent.LeaseToken != arg.LeaseToken {
		return 0, nil
	}
	q.intent.Action = arg.NextAction
	q.intent.LeaseToken = pgtype.UUID{}
	q.transitions++
	return 1, nil
}

func (q *workerTestQueries) counts() (transitions, deletes, expires, failures, deadLetters int) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.transitions, q.deletes, q.expires, q.failures, q.deadLetters
}

func (q *workerTestQueries) rateLimitState() (claimCalls, deferrals int, attemptCount int32, deferredUntil time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.claimCalls, q.deferrals, q.intent.AttemptCount, q.deferredUntil.Time
}

func workerTestIntent(action string) db.SeatCapacityOutbox {
	return db.SeatCapacityOutbox{
		WorkspaceID: uuidToTestPG(uuid.New()), OperationToken: uuidToTestPG(uuid.New()),
		Action: action, InvitationID: uuidToTestPG(uuid.New()), LeaseToken: uuidToTestPG(uuid.New()),
	}
}

func uuidToTestPG(value uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: value, Valid: true}
}

func recoveredDecision(state string) Decision {
	return Decision{Managed: true, Operation: &Operation{State: state}}
}

func TestRecoverConsumingTransitionsAbandonedOperationToRelease(t *testing.T) {
	intent := workerTestIntent(ActionConsumeInvitation)
	queries := &workerTestQueries{intent: intent}
	worker := newWorker(queries, &workerTestExecutor{decision: recoveredDecision("consuming")}, WorkerConfig{})

	if err := worker.recoverConsuming(context.Background(), intent, uuidFromPG(intent.WorkspaceID), uuidFromPG(intent.OperationToken)); err != nil {
		t.Fatal(err)
	}
	transitions, deletes, expires, _, _ := queries.counts()
	if transitions != 1 || deletes != 0 || expires != 1 {
		t.Fatalf("transitions=%d deletes=%d expires=%d, want 1/0/1", transitions, deletes, expires)
	}
	if queries.intent.Action != ActionRelease {
		t.Fatalf("action=%q, want %q", queries.intent.Action, ActionRelease)
	}
}

func TestRecoverConsumingUsedDeletesWithoutReleasing(t *testing.T) {
	intent := workerTestIntent(ActionConsumeInvitation)
	queries := &workerTestQueries{intent: intent}
	worker := newWorker(queries, &workerTestExecutor{decision: recoveredDecision("used")}, WorkerConfig{})

	if err := worker.recoverConsuming(context.Background(), intent, uuidFromPG(intent.WorkspaceID), uuidFromPG(intent.OperationToken)); err != nil {
		t.Fatal(err)
	}
	transitions, deletes, expires, _, _ := queries.counts()
	if transitions != 0 || deletes != 1 || expires != 0 {
		t.Fatalf("transitions=%d deletes=%d expires=%d, want 0/1/0", transitions, deletes, expires)
	}
}

func TestRecoverReserveKeepsPendingInvitationReservation(t *testing.T) {
	intent := workerTestIntent(ActionReserveInvitation)
	queries := &workerTestQueries{
		intent: intent,
		invitation: db.WorkspaceInvitation{
			ID: intent.InvitationID, Status: "pending",
		},
	}
	worker := newWorker(queries, &workerTestExecutor{decision: recoveredDecision("reserved")}, WorkerConfig{})

	if err := worker.recoverReserve(context.Background(), intent, uuidFromPG(intent.WorkspaceID), uuidFromPG(intent.OperationToken)); err != nil {
		t.Fatal(err)
	}
	transitions, deletes, expires, _, _ := queries.counts()
	if transitions != 0 || deletes != 1 || expires != 0 {
		t.Fatalf("transitions=%d deletes=%d expires=%d, want 0/1/0", transitions, deletes, expires)
	}
}

func TestRecoveryCleansUnknownOrUnmanagedOperations(t *testing.T) {
	tests := []struct {
		name     string
		decision Decision
		err      error
	}{
		{name: "not found", err: &HTTPError{StatusCode: http.StatusNotFound}},
		{name: "unmanaged", decision: Decision{Managed: false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := workerTestIntent(ActionConsumeInvitation)
			queries := &workerTestQueries{intent: intent}
			worker := newWorker(queries, &workerTestExecutor{decision: tt.decision, err: tt.err}, WorkerConfig{})

			if err := worker.recoverConsuming(context.Background(), intent, uuidFromPG(intent.WorkspaceID), uuidFromPG(intent.OperationToken)); err != nil {
				t.Fatal(err)
			}
			_, deletes, expires, _, _ := queries.counts()
			if deletes != 1 || expires != 0 {
				t.Fatalf("deletes=%d expires=%d, want 1/0", deletes, expires)
			}
		})
	}
}

func TestConcurrentRecoveryOnlyOneReplicaTransitionsIntent(t *testing.T) {
	intent := workerTestIntent(ActionConsumeInvitation)
	queries := &workerTestQueries{intent: intent}
	executor := &workerTestExecutor{decision: recoveredDecision("consuming")}
	workerA := newWorker(queries, executor, WorkerConfig{})
	workerB := newWorker(queries, executor, WorkerConfig{})

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, worker := range []*Worker{workerA, workerB} {
		wg.Add(1)
		go func(w *Worker) {
			defer wg.Done()
			errs <- w.recoverConsuming(context.Background(), intent, uuidFromPG(intent.WorkspaceID), uuidFromPG(intent.OperationToken))
		}(worker)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	transitions, _, expires, _, _ := queries.counts()
	if transitions != 1 || expires != 1 {
		t.Fatalf("transitions=%d expires=%d, want 1/1", transitions, expires)
	}
}

func TestWorkerDeadLettersAfterMaximumAttempts(t *testing.T) {
	intent := workerTestIntent(ActionConfirm)
	intent.AttemptCount = 1
	queries := &workerTestQueries{intent: intent, claimAvailable: true}
	worker := newWorker(queries, &workerTestExecutor{err: errors.New("cloud unavailable")}, WorkerConfig{
		MaxAttempts: 2,
	})

	if err := worker.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, _, failures, deadLetters := queries.counts()
	if failures != 0 || deadLetters != 1 {
		t.Fatalf("failures=%d deadLetters=%d, want 0/1", failures, deadLetters)
	}
}

func TestWorkerDefersCloudRateLimitWithoutSpendingRetryBudget(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	intent := workerTestIntent(ActionConfirm)
	intent.AttemptCount = 9
	queries := &workerTestQueries{intent: intent, claimAvailable: true}
	worker := newWorker(queries, &workerTestExecutor{err: &HTTPError{
		StatusCode: http.StatusTooManyRequests,
		Code:       "capacity_rate_limited",
		RetryAfter: 3 * time.Second,
	}}, WorkerConfig{MaxAttempts: 10})
	worker.now = func() time.Time { return now }

	if err := worker.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	claimCalls, deferrals, attemptCount, deferredUntil := queries.rateLimitState()
	_, _, _, failures, deadLetters := queries.counts()
	if claimCalls != 1 {
		t.Fatalf("claim calls=%d, want 1 so the rate-limited batch stops immediately", claimCalls)
	}
	if deferrals != 1 || failures != 0 || deadLetters != 0 {
		t.Fatalf("deferrals=%d failures=%d deadLetters=%d, want 1/0/0", deferrals, failures, deadLetters)
	}
	if attemptCount != 9 {
		t.Fatalf("attempt count=%d, want unchanged value 9", attemptCount)
	}
	if want := now.Add(3 * time.Second); !deferredUntil.Equal(want) {
		t.Fatalf("deferred until=%s, want %s", deferredUntil, want)
	}
}

func TestWorkerWorkspaceRateLimitDoesNotStopOtherTenants(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	limitedWorkspace, healthyWorkspace := uuid.New(), uuid.New()
	limitedA := workerTestIntent(ActionConfirm)
	limitedA.WorkspaceID = uuidToTestPG(limitedWorkspace)
	limitedB := workerTestIntent(ActionConfirm)
	limitedB.WorkspaceID = uuidToTestPG(limitedWorkspace)
	healthy := workerTestIntent(ActionConfirm)
	healthy.WorkspaceID = uuidToTestPG(healthyWorkspace)
	queries := &workerTestQueries{intents: []db.SeatCapacityOutbox{limitedA, limitedB, healthy}}
	executor := &workerTestExecutor{
		decision: Decision{Managed: true, Allowed: true},
		errorsByWorkspace: map[uuid.UUID]error{
			limitedWorkspace: &HTTPError{
				StatusCode:     http.StatusTooManyRequests,
				RetryAfter:     2 * time.Second,
				RateLimitScope: RateLimitScopeWorkspace,
			},
		},
	}
	worker := newWorker(queries, executor, WorkerConfig{BatchSize: 10})
	worker.now = func() time.Time { return now }

	if err := worker.ReconcileOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := executor.confirmWorkspaces; len(got) != 2 || got[0] != limitedWorkspace || got[1] != healthyWorkspace {
		t.Fatalf("Cloud confirm workspaces = %v, want one limited call followed by healthy tenant", got)
	}
	if queries.deferrals != 2 || queries.deletes != 1 {
		t.Fatalf("deferrals=%d deletes=%d, want 2/1", queries.deferrals, queries.deletes)
	}
	if queries.claimCalls != 4 {
		t.Fatalf("claim calls=%d, want three intents plus end-of-queue check", queries.claimCalls)
	}
	for _, deferred := range queries.deferredUntils {
		if want := now.Add(2 * time.Second); !deferred.Time.Equal(want) {
			t.Fatalf("deferred until=%s, want %s", deferred.Time, want)
		}
	}
}

func TestUnavailableExecutorDisablesWorkerRecovery(t *testing.T) {
	unavailable := NewUnavailable(errors.New("invalid capacity credentials"))
	if CanRunWorker(unavailable) {
		t.Fatal("unavailable executor can run recovery worker")
	}
	worker := newWorker(&workerTestQueries{}, unavailable, WorkerConfig{})
	if worker.Enabled() {
		t.Fatal("worker is enabled with unavailable executor")
	}
	if !CanRunWorker(&workerTestExecutor{}) {
		t.Fatal("configured executor cannot run recovery worker")
	}
}

func TestWorkerUsesWorkspaceSerializationBeforeCloudCall(t *testing.T) {
	intent := workerTestIntent(ActionConfirm)
	queries := &workerTestQueries{intent: intent}
	locker := &workerTestLocker{}
	worker := newWorker(queries, &workerTestExecutor{decision: Decision{Managed: true, Allowed: true}}, WorkerConfig{})
	worker.workspaceLocker = locker

	if err := worker.settleWithWorkspaceLimit(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	if locker.locks != 1 || locker.unlocks != 1 {
		t.Fatalf("locks=%d unlocks=%d, want 1/1", locker.locks, locker.unlocks)
	}
}

func TestWorkerSkipsCloudCallWhenClaimWasReactivatedBeforeWorkspaceLock(t *testing.T) {
	intent := workerTestIntent(ActionConfirm)
	queries := &workerTestQueries{intent: intent}
	executor := &workerTestExecutor{decision: Decision{Managed: true, Allowed: true}}
	worker := newWorker(queries, executor, WorkerConfig{})
	worker.workspaceLocker = &workerTestLocker{}
	queries.intent.LeaseToken = pgtype.UUID{}

	if err := worker.settleWithWorkspaceLimit(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	if len(executor.confirmWorkspaces) != 0 {
		t.Fatalf("stale worker made %d Cloud confirm calls, want 0", len(executor.confirmWorkspaces))
	}
}

func TestRecoveryDueAllowsRetryableRequestFailuresToSettle(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := RecoveryDue(now).Time.Sub(now); got != 5*time.Minute {
		t.Fatalf("RecoveryDue delay=%s, want 5m", got)
	}
}
