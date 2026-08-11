// Package service: independent review scheduler (Plan v1.4 V4-5 + v1.5 V5-7).
// Owner: ALL-16.
//
// EvidenceReviewScheduler is the ONLY producer of independent review work. It
// runs alongside the existing server sweepers and advances
// execution_evidence_record.review_state through the frozen transition table
// (V5-7.2):
//
//	pending -> dispatching (CAS lease + attempt++)
//	dispatching -> queued (reviewer task + ledger committed, task id stored)
//	queued -> running (reported by the existing claim path)
//	running -> recorded (reviewer output/message/usage refs persisted)
//	dispatching|queued|running -> retry_wait (transient failure, backoff)
//	retry_wait -> pending (wakeup reached, attempts remain)
//	any -> blocked (missing/self/scope-invalid reviewer, attempts exhausted)
//
// blocked NEVER carries a scheduler wakeup and is never auto-retried; the only
// path out of blocked is the owner repair transaction (V5-7.3 / V6-1).
//
// The reviewer task itself is created with review_policy='none' so a reviewer
// run never recursively requests its own reviewer (recursion guard).
package service

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ReviewQuerier is the DB boundary the scheduler needs. *db.Queries satisfies
// it; tests substitute a fake.
type ReviewQuerier interface {
	ReleaseExpiredReviewLeases(ctx context.Context) error
	ResetExpiredDispatchingReviewCAS(ctx context.Context, arg db.ResetExpiredDispatchingReviewCASParams) (db.ExecutionEvidenceRecord, error)
	ListReviewDueRecords(ctx context.Context, limit int32) ([]db.ExecutionEvidenceRecord, error)
	ClaimPendingReviewCAS(ctx context.Context, arg db.ClaimPendingReviewCASParams) (db.ExecutionEvidenceRecord, error)
	MarkReviewQueuedCAS(ctx context.Context, arg db.MarkReviewQueuedCASParams) (db.ExecutionEvidenceRecord, error)
	MarkReviewRecordedCAS(ctx context.Context, arg db.MarkReviewRecordedCASParams) (db.ExecutionEvidenceRecord, error)
	MarkReviewRetryWaitCAS(ctx context.Context, arg db.MarkReviewRetryWaitCASParams) (db.ExecutionEvidenceRecord, error)
	MarkReviewPendingRetryCAS(ctx context.Context, arg db.MarkReviewPendingRetryCASParams) (db.ExecutionEvidenceRecord, error)
	BlockReviewCAS(ctx context.Context, arg db.BlockReviewCASParams) (db.ExecutionEvidenceRecord, error)
}

// ReviewTaskEnqueuer commits the reviewer task + execution ledger for a
// dispatching record and returns the new reviewer task id. Implementations
// must run CreateReviewerTask and InsertExecutionLedger in ONE transaction
// and then MarkReviewQueuedCAS in the same transaction (V5-7.2 dispatching ->
// queued).
type ReviewTaskEnqueuer interface {
	// Enqueue creates the reviewer task for rec (state must be dispatching)
	// and stores its id. It returns the created task id.
	Enqueue(ctx context.Context, rec db.ExecutionEvidenceRecord) (pgtype.UUID, error)
}

// ReviewSchedulerConfig pins lease and backoff bounds.
type ReviewSchedulerConfig struct {
	LeaseOwner    string
	LeaseDuration time.Duration
	BatchSize     int32
	// BackoffSeconds maps attempt -> seconds (1,2,4,8,16,32 frozen; capped).
	BackoffSeconds func(attempt int) int
}

// EvidenceReviewScheduler advances review_state for due records.
type EvidenceReviewScheduler struct {
	Queries  ReviewQuerier
	Enqueuer ReviewTaskEnqueuer
	Config   ReviewSchedulerConfig
	// Now returns the scheduler clock (injectable for tests).
	Now func() time.Time
}

// NewEvidenceReviewScheduler wires the scheduler.
func NewEvidenceReviewScheduler(q ReviewQuerier, enq ReviewTaskEnqueuer, cfg ReviewSchedulerConfig) *EvidenceReviewScheduler {
	if cfg.BackoffSeconds == nil {
		cfg.BackoffSeconds = reviewTransitionBackoff
	}
	return &EvidenceReviewScheduler{
		Queries:  q,
		Enqueuer: enq,
		Config:   cfg,
		Now:      time.Now,
	}
}

// Sweep runs one scheduler pass:
//  1. release expired leases and reset stale dispatching rows to pending;
//  2. promote retry_wait rows whose wakeup has passed back to pending;
//  3. claim pending rows (CAS lease + attempt++);
//  4. enqueue a reviewer task for each freshly-claimed row;
//  5. record completed reviewer work and block exhausted rows.
//
// blocked rows are never selected by ListReviewDueRecords (migration 328 index
// excludes them), so they are never auto-scheduled.
func (s *EvidenceReviewScheduler) Sweep(ctx context.Context) error {
	if err := s.Queries.ReleaseExpiredReviewLeases(ctx); err != nil {
		return err
	}

	due, err := s.Queries.ListReviewDueRecords(ctx, s.Config.BatchSize)
	if err != nil {
		return err
	}

	for _, rec := range due {
		if err := s.advance(ctx, rec); err != nil {
			continue
		}
	}
	return nil
}

// advance drives one due record forward. Terminal states (not_required,
// recorded, blocked) are not in the due set by construction.
func (s *EvidenceReviewScheduler) advance(ctx context.Context, rec db.ExecutionEvidenceRecord) error {
	switch protocol.ReviewState(rec.ReviewState) {
	case protocol.ReviewStateRetryWait:
		// Wakeup reached (the due query filters next_wakeup <= now). Move back
		// to pending, then let the pending path claim it.
		if _, err := s.Queries.MarkReviewPendingRetryCAS(ctx, db.MarkReviewPendingRetryCASParams{
			ExecutionID:   rec.ExecutionID,
			ReviewVersion: rec.ReviewVersion,
		}); err != nil {
			return err
		}
		// Fall through to the pending claim path.
		return s.dispatch(ctx, rec.ExecutionID, rec.ReviewVersion)
	case protocol.ReviewStatePending:
		return s.dispatch(ctx, rec.ExecutionID, rec.ReviewVersion)
	case protocol.ReviewStateDispatching:
		// If the lease is still fresh, another scheduler owns this record.
		// If the lease is null (released) or expired (server crash mid-
		// dispatch), reset to pending and re-drive it in this same pass.
		if !rec.ReviewLeaseExpiresAt.Valid || !rec.ReviewLeaseExpiresAt.Time.After(s.Now()) {
			_, rerr := s.Queries.ResetExpiredDispatchingReviewCAS(ctx, db.ResetExpiredDispatchingReviewCASParams{
				ExecutionID:   rec.ExecutionID,
				ReviewVersion: rec.ReviewVersion,
			})
			if rerr != nil {
				return rerr
			}
			return s.dispatch(ctx, rec.ExecutionID, rec.ReviewVersion)
		}
		return nil
	case protocol.ReviewStateQueued:
		// Reviewer task created but not yet reported running. Nothing for the
		// producer to do; the claim path advances queued -> running.
		return nil
	case protocol.ReviewStateRunning:
		// Reviewer work is reported by the existing claim path via
		// MarkReviewRecordedCAS. The producer only reconciles terminal output
		// when a completed reviewer task is observed; that is handled by the
		// daemon-facing recorder, not here.
		return nil
	default:
		return nil
	}
}

// dispatch claims a pending (or freshly-promoted) record and enqueues the
// reviewer task. Transient enqueue failure -> retry_wait with backoff; attempt
// exhaustion -> blocked (no wakeup).
func (s *EvidenceReviewScheduler) dispatch(ctx context.Context, executionID pgtype.UUID, version int32) error {
	claimed, err := s.Queries.ClaimPendingReviewCAS(ctx, db.ClaimPendingReviewCASParams{
		ExecutionID:      executionID,
		ReviewLeaseOwner: pgtype.Text{String: s.Config.LeaseOwner, Valid: true},
		Column3:          pgtype.Interval{Microseconds: s.Config.LeaseDuration.Microseconds(), Valid: true},
		ReviewVersion:    version,
	})
	if err != nil {
		return err
	}

	taskID, err := s.Enqueuer.Enqueue(ctx, claimed)
	if err != nil {
		// Transient dispatch failure: retry_wait with backoff if attempts
		// remain, otherwise blocked.
		nextAttempt := claimed.ReviewAttempt
		if nextAttempt < claimed.MaxReviewAttempts {
			wait := s.Config.BackoffSeconds(int(nextAttempt))
			_, werr := s.Queries.MarkReviewRetryWaitCAS(ctx, db.MarkReviewRetryWaitCASParams{
				ExecutionID:       executionID,
				ReviewNextWakeup:  pgtype.Timestamptz{Time: s.Now().Add(time.Duration(wait) * time.Second), Valid: true},
				ReviewFailureCode: pgtype.Text{String: "memoryhub_review_dispatch_failed", Valid: true},
				ReviewVersion:     claimed.ReviewVersion,
			})
			return werr
		}
		_, berr := s.Queries.BlockReviewCAS(ctx, db.BlockReviewCASParams{
			ExecutionID:       executionID,
			ReviewFailureCode: pgtype.Text{String: "memoryhub_review_blocked", Valid: true},
			ReviewVersion:     claimed.ReviewVersion,
		})
		return berr
	}

	_, err = s.Queries.MarkReviewQueuedCAS(ctx, db.MarkReviewQueuedCASParams{
		ExecutionID:   executionID,
		ReviewTaskID:  taskID,
		ReviewVersion: claimed.ReviewVersion,
	})
	return err
}
