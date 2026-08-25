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
	decision Decision
	err      error
	confirms int
}

type workerTestLocker struct {
	locks   int
	unlocks int
}

func (l *workerTestLocker) Lock(context.Context, uuid.UUID) (db.DBTX, func(), error) {
	l.locks++
	return nil, func() { l.unlocks++ }, nil
}

func (e *workerTestExecutor) Enabled() bool { return true }
func (e *workerTestExecutor) ReserveInvitation(context.Context, uuid.UUID, uuid.UUID, time.Time) (Decision, error) {
	return Decision{}, nil
}
func (e *workerTestExecutor) ClaimShareJoin(context.Context, uuid.UUID, uuid.UUID) (Decision, error) {
	return Decision{}, nil
}
func (e *workerTestExecutor) Consume(context.Context, uuid.UUID, uuid.UUID) (Decision, error) {
	return Decision{}, nil
}
func (e *workerTestExecutor) Confirm(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (Decision, error) {
	e.confirms++
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
	claimAvailable  bool
	invitation      db.WorkspaceInvitation
	invitationError error
	stats           []db.SeatCapacityOutboxStatsRow

	transitions int
	deletes     int
	expires     int
	failures    int
	deadLetters int
}

func (q *workerTestQueries) ClaimNextDueSeatCapacityIntent(context.Context, pgtype.Timestamptz) (db.SeatCapacityOutbox, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.claimAvailable {
		return db.SeatCapacityOutbox{}, pgx.ErrNoRows
	}
	q.claimAvailable = false
	q.intent.LeaseToken = uuidToTestPG(uuid.New())
	return q.intent, nil
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
	if executor.confirms != 0 {
		t.Fatalf("stale worker made %d Cloud confirm calls, want 0", executor.confirms)
	}
}

func TestRecoveryDueAllowsRetryableRequestFailuresToSettle(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := RecoveryDue(now).Time.Sub(now); got != 5*time.Minute {
		t.Fatalf("RecoveryDue delay=%s, want 5m", got)
	}
}
