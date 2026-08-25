package seatcapacity

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	ActionReserveInvitation = "reserve_invitation"
	ActionConsumeInvitation = "consume_invitation"
	ActionClaimShareJoin    = "claim_share_join"
	ActionConfirm           = "confirm"
	ActionRelease           = "release"
	ActionReleaseMember     = "release_member"

	defaultReconcileInterval = 30 * time.Second
	defaultRecoveryGrace     = 5 * time.Minute
	defaultClaimLease        = 5 * time.Minute
	defaultWorkspaceLockWait = 30 * time.Second
	defaultBatchSize         = 100
	defaultMaxAttempts       = 10
)

type WorkerConfig struct {
	ReconcileInterval time.Duration
	BatchSize         int32
	MaxAttempts       int32
	Logger            *slog.Logger
	Metrics           WorkerMetrics
}

type workerQueries interface {
	ClaimNextDueSeatCapacityIntent(context.Context, pgtype.Timestamptz) (db.SeatCapacityOutbox, error)
	DeleteClaimedSeatCapacityIntent(context.Context, db.DeleteClaimedSeatCapacityIntentParams) (int64, error)
	ExpireInvitationForCapacityRecovery(context.Context, pgtype.UUID) error
	GetClaimedSeatCapacityIntent(context.Context, db.GetClaimedSeatCapacityIntentParams) (db.SeatCapacityOutbox, error)
	GetInvitation(context.Context, pgtype.UUID) (db.WorkspaceInvitation, error)
	MarkClaimedSeatCapacityIntentDeadLettered(context.Context, db.MarkClaimedSeatCapacityIntentDeadLetteredParams) (int64, error)
	MarkClaimedSeatCapacityIntentFailed(context.Context, db.MarkClaimedSeatCapacityIntentFailedParams) (int64, error)
	SeatCapacityOutboxStats(context.Context) ([]db.SeatCapacityOutboxStatsRow, error)
	TransitionClaimedSeatCapacityIntent(context.Context, db.TransitionClaimedSeatCapacityIntentParams) (int64, error)
}

type WorkerMetrics interface {
	ResetOutbox()
	SetOutbox(action string, pending, deadLettered int64, oldestPendingAgeSeconds float64)
}

// Worker settles durable product-side intents. Each row is claimed atomically
// before a Cloud request, so API replicas do not amplify the same backlog.
type Worker struct {
	queries           workerQueries
	executor          Executor
	reconcileInterval time.Duration
	batchSize         int32
	maxAttempts       int32
	logger            *slog.Logger
	metrics           WorkerMetrics
	workspaceLocker   WorkspaceLocker
	now               func() time.Time
}

func NewWorker(queries *db.Queries, executor Executor, locker WorkspaceLocker, cfg WorkerConfig) *Worker {
	worker := newWorker(queries, executor, cfg)
	worker.workspaceLocker = locker
	return worker
}

func newWorker(queries workerQueries, executor Executor, cfg WorkerConfig) *Worker {
	interval := cfg.ReconcileInterval
	if interval <= 0 {
		interval = defaultReconcileInterval
	}
	batch := cfg.BatchSize
	if batch <= 0 {
		batch = defaultBatchSize
	}
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultMaxAttempts
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		queries: queries, executor: executor, reconcileInterval: interval,
		batchSize: batch, maxAttempts: maxAttempts, logger: logger,
		metrics: cfg.Metrics, now: time.Now,
	}
}

func (w *Worker) Enabled() bool {
	return w != nil && w.queries != nil && w.executor != nil && w.executor.Enabled()
}

func (w *Worker) Run(ctx context.Context) {
	if !w.Enabled() {
		return
	}
	ticker := time.NewTicker(w.reconcileInterval)
	defer ticker.Stop()
	for {
		if err := w.ReconcileOnce(ctx); err != nil && ctx.Err() == nil {
			w.logger.WarnContext(ctx, "seat capacity outbox reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) ReconcileOnce(ctx context.Context) error {
	if !w.Enabled() {
		return nil
	}
	for i := int32(0); i < w.batchSize; i++ {
		intent, err := w.queries.ClaimNextDueSeatCapacityIntent(ctx, pgtype.Timestamptz{
			Time: w.now().Add(defaultClaimLease), Valid: true,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return err
		}
		settleErr := w.settleWithWorkspaceLimit(ctx, intent)
		if settleErr != nil {
			w.recordFailure(ctx, intent, settleErr)
		}
	}
	return w.refreshMetrics(ctx)
}

func (w *Worker) settleWithWorkspaceLimit(ctx context.Context, intent db.SeatCapacityOutbox) error {
	if w.workspaceLocker == nil {
		return w.settle(ctx, intent)
	}
	lockCtx, cancel := context.WithTimeout(ctx, defaultWorkspaceLockWait)
	defer cancel()
	lockedDB, unlock, err := w.workspaceLocker.Lock(lockCtx, uuidFromPG(intent.WorkspaceID))
	if err != nil {
		return err
	}
	defer unlock()
	lockedQueries := w.queries
	if lockedDB != nil {
		lockedQueries = db.New(lockedDB)
	}
	current, err := lockedQueries.GetClaimedSeatCapacityIntent(ctx, db.GetClaimedSeatCapacityIntentParams{
		OperationToken: intent.OperationToken, Action: intent.Action, LeaseToken: intent.LeaseToken,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	lockedWorker := *w
	lockedWorker.queries = lockedQueries
	return lockedWorker.settle(ctx, current)
}

func (w *Worker) settle(ctx context.Context, intent db.SeatCapacityOutbox) error {
	if !intent.LeaseToken.Valid {
		return errors.New("claimed seat capacity intent omitted lease token")
	}
	workspaceID := uuidFromPG(intent.WorkspaceID)
	token := uuidFromPG(intent.OperationToken)
	switch intent.Action {
	case ActionReserveInvitation:
		return w.recoverReserve(ctx, intent, workspaceID, token)
	case ActionConsumeInvitation, ActionClaimShareJoin:
		return w.recoverConsuming(ctx, intent, workspaceID, token)
	case ActionConfirm:
		decision, err := w.executor.Confirm(ctx, workspaceID, token, uuidFromPG(intent.MemberID))
		if err != nil {
			return err
		}
		if !decision.Allowed {
			return errors.New("capacity confirm rejected in state " + decision.Reason)
		}
		return w.deleteCurrent(ctx, intent)
	case ActionRelease:
		decision, err := w.executor.Release(ctx, workspaceID, token)
		if err != nil && !IsNotFound(err) {
			return err
		}
		if err == nil && decision.Managed && !decision.Allowed && decision.Reason != "released" && decision.Reason != "denied" {
			return errors.New("capacity release rejected in state " + decision.Reason)
		}
		return w.deleteCurrent(ctx, intent)
	case ActionReleaseMember:
		decision, err := w.executor.ReleaseMember(ctx, workspaceID, uuidFromPG(intent.MemberID))
		if err != nil && !IsNotFound(err) {
			return err
		}
		if err == nil && decision.Managed && !decision.Allowed && decision.Reason != "released" {
			return errors.New("capacity member release rejected in state " + decision.Reason)
		}
		return w.deleteCurrent(ctx, intent)
	default:
		return errors.New("unknown seat capacity outbox action")
	}
}

func (w *Worker) recoverReserve(ctx context.Context, intent db.SeatCapacityOutbox, workspaceID, token uuid.UUID) error {
	decision, err := w.executor.GetOperation(ctx, workspaceID, token)
	if IsNotFound(err) || (err == nil && !decision.Managed) {
		return w.deleteCurrent(ctx, intent)
	}
	if err != nil {
		return err
	}
	if decision.Operation == nil {
		return errors.New("managed capacity operation response omitted operation")
	}
	switch decision.Operation.State {
	case "denied", "released":
		return w.deleteCurrent(ctx, intent)
	case "reserved":
		invitationID := intent.InvitationID
		if !invitationID.Valid {
			_, transitionErr := w.transition(ctx, intent, ActionRelease, pgtype.UUID{})
			return transitionErr
		}
		invitation, getErr := w.queries.GetInvitation(ctx, invitationID)
		if getErr == nil && invitation.Status == "pending" {
			return w.deleteCurrent(ctx, intent)
		}
		if getErr != nil && !errors.Is(getErr, pgx.ErrNoRows) {
			return getErr
		}
		_, transitionErr := w.transition(ctx, intent, ActionRelease, pgtype.UUID{})
		return transitionErr
	default:
		return errors.New("unexpected recovered invitation reservation state " + decision.Operation.State)
	}
}

func (w *Worker) recoverConsuming(ctx context.Context, intent db.SeatCapacityOutbox, workspaceID, token uuid.UUID) error {
	decision, err := w.executor.GetOperation(ctx, workspaceID, token)
	if IsNotFound(err) || (err == nil && !decision.Managed) {
		return w.deleteCurrent(ctx, intent)
	}
	if err != nil {
		return err
	}
	if decision.Operation == nil {
		return errors.New("managed capacity operation response omitted operation")
	}
	switch decision.Operation.State {
	case "reserved":
		// The consume request never took effect. Keep the invitation usable.
		return w.deleteCurrent(ctx, intent)
	case "used":
		// A concurrent request already committed and confirmed the member. A
		// stale consuming worker must not try to release that used seat.
		return w.deleteCurrent(ctx, intent)
	case "denied", "released":
		if intent.Action == ActionConsumeInvitation && intent.InvitationID.Valid {
			_ = w.queries.ExpireInvitationForCapacityRecovery(ctx, intent.InvitationID)
		}
		return w.deleteCurrent(ctx, intent)
	case "consuming":
		// No product transaction committed: that transaction would atomically
		// change this row to confirm. Retire the abandoned user request rather
		// than hold capacity forever.
		changed, err := w.transition(ctx, intent, ActionRelease, pgtype.UUID{})
		if err != nil || !changed {
			return err
		}
		if intent.Action == ActionConsumeInvitation && intent.InvitationID.Valid {
			if err := w.queries.ExpireInvitationForCapacityRecovery(ctx, intent.InvitationID); err != nil {
				return err
			}
		}
		return nil
	default:
		return errors.New("unexpected recovered consuming capacity state " + decision.Operation.State)
	}
}

func (w *Worker) transition(ctx context.Context, intent db.SeatCapacityOutbox, action string, memberID pgtype.UUID) (bool, error) {
	rows, err := w.queries.TransitionClaimedSeatCapacityIntent(ctx, db.TransitionClaimedSeatCapacityIntentParams{
		NextAction: action, CurrentAction: intent.Action, MemberID: memberID, OperationToken: intent.OperationToken,
		NextAttemptAt: pgtype.Timestamptz{Time: w.now(), Valid: true}, LeaseToken: intent.LeaseToken,
	})
	return rows == 1, err
}

func (w *Worker) deleteCurrent(ctx context.Context, intent db.SeatCapacityOutbox) error {
	_, err := w.queries.DeleteClaimedSeatCapacityIntent(ctx, db.DeleteClaimedSeatCapacityIntentParams{
		OperationToken: intent.OperationToken,
		Action:         intent.Action,
		LeaseToken:     intent.LeaseToken,
	})
	return err
}

func (w *Worker) recordFailure(ctx context.Context, intent db.SeatCapacityOutbox, settleErr error) {
	if intent.AttemptCount+1 >= w.maxAttempts {
		rows, err := w.queries.MarkClaimedSeatCapacityIntentDeadLettered(ctx, db.MarkClaimedSeatCapacityIntentDeadLetteredParams{
			LastError: settleErr.Error(), OperationToken: intent.OperationToken, Action: intent.Action,
			LeaseToken: intent.LeaseToken,
		})
		if err != nil {
			w.logger.WarnContext(ctx, "seat capacity outbox dead letter could not be recorded", "error", err)
			return
		}
		if rows == 1 {
			w.logger.ErrorContext(ctx, "seat capacity outbox intent moved to dead letter",
				"workspace_id", workspaceIDString(intent.WorkspaceID), "action", intent.Action,
				"attempt", intent.AttemptCount+1, "error", settleErr)
		}
		return
	}
	backoff := 5 * time.Second
	for i := int32(0); i < intent.AttemptCount && backoff < 5*time.Minute; i++ {
		backoff *= 2
	}
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	rows, err := w.queries.MarkClaimedSeatCapacityIntentFailed(ctx, db.MarkClaimedSeatCapacityIntentFailedParams{
		LastError: settleErr.Error(), NextAttemptAt: pgtype.Timestamptz{Time: w.now().Add(backoff), Valid: true},
		OperationToken: intent.OperationToken, Action: intent.Action, LeaseToken: intent.LeaseToken,
	})
	if err != nil {
		w.logger.WarnContext(ctx, "seat capacity outbox failure could not be recorded", "error", err)
	}
	if rows == 1 && (intent.AttemptCount == 0 || (intent.AttemptCount+1)%10 == 0) {
		w.logger.WarnContext(ctx, "seat capacity outbox intent remains unsettled",
			"workspace_id", workspaceIDString(intent.WorkspaceID), "action", intent.Action,
			"attempt", intent.AttemptCount+1, "error", settleErr)
	}
}

func (w *Worker) refreshMetrics(ctx context.Context) error {
	if w.metrics == nil {
		return nil
	}
	stats, err := w.queries.SeatCapacityOutboxStats(ctx)
	if err != nil {
		return err
	}
	w.metrics.ResetOutbox()
	for _, stat := range stats {
		w.metrics.SetOutbox(stat.Action, stat.PendingCount, stat.DeadLetteredCount, stat.OldestPendingAgeSeconds)
	}
	return nil
}

func uuidFromPG(value pgtype.UUID) uuid.UUID {
	if !value.Valid {
		return uuid.Nil
	}
	return uuid.UUID(value.Bytes)
}

func workspaceIDString(value pgtype.UUID) string { return uuidFromPG(value).String() }

func RecoveryDue(now time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: now.Add(defaultRecoveryGrace), Valid: true}
}

func RetryDue(now time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: now, Valid: true}
}
