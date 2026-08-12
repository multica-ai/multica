package service

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
	corpusTransferCleanupSweepInterval = time.Minute
	corpusTransferCleanupLease         = 2 * time.Minute
	corpusTransferCleanupLimit         = 25
	corpusTransferCleanupDeleteTimeout = 30 * time.Second
	corpusTransferCleanupBackoffBase   = time.Minute
	corpusTransferCleanupBackoffCap    = time.Hour
)

// Re-delete after successful removal so an abandoned object-store PUT that
// materializes late is still reclaimed. The durable row is released only after
// the whole widening window has elapsed.
var corpusTransferCleanupRedelete = []time.Duration{
	15 * time.Minute,
	time.Hour,
	6 * time.Hour,
	24 * time.Hour,
}

type CorpusTransferCleanupQueries interface {
	ClaimNextCorpusTransferForCleanup(context.Context, db.ClaimNextCorpusTransferForCleanupParams) (db.CorpusTransfer, error)
	RetryCorpusTransferCleanup(context.Context, db.RetryCorpusTransferCleanupParams) (db.CorpusTransfer, error)
	ScheduleCorpusTransferCleanupPass(context.Context, db.ScheduleCorpusTransferCleanupPassParams) (db.CorpusTransfer, error)
	CompleteCorpusTransferCleanup(context.Context, db.CompleteCorpusTransferCleanupParams) (db.CorpusTransfer, error)
	DeleteOrphanedCorpusTransferAfterCleanup(context.Context, db.DeleteOrphanedCorpusTransferAfterCleanupParams) (db.CorpusTransfer, error)
}

// CorpusTransferReconciler consumes durable cleanup intents for failed or
// expired transfers. Claims are lease-fenced, storage failures back off, and
// successful deletes are repeated on a widening schedule before the intent is
// cleared, making process death and late object materialization recoverable.
type CorpusTransferReconciler struct {
	Queries CorpusTransferCleanupQueries
	Storage MediaObjectDeleter
	Logger  *slog.Logger
}

func (r *CorpusTransferReconciler) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

func (r *CorpusTransferReconciler) Run(ctx context.Context) {
	r.RunOnce(ctx)
	ticker := time.NewTicker(corpusTransferCleanupSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.RunOnce(ctx)
		}
	}
}

func (r *CorpusTransferReconciler) RunOnce(ctx context.Context) {
	if r.Queries == nil || r.Storage == nil {
		return
	}
	for i := 0; i < corpusTransferCleanupLimit; i++ {
		leaseToken := pgtype.UUID{Bytes: uuid.New(), Valid: true}
		row, err := r.Queries.ClaimNextCorpusTransferForCleanup(ctx, db.ClaimNextCorpusTransferForCleanupParams{
			CleanupLeaseToken: leaseToken,
			CleanupLease:      pgInterval(corpusTransferCleanupLease),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		if err != nil {
			if ctx.Err() == nil {
				r.logger().Warn("corpus transfer cleanup claim failed", "error", err)
			}
			return
		}
		r.settle(ctx, row, leaseToken)
	}
}

func (r *CorpusTransferReconciler) settle(ctx context.Context, row db.CorpusTransfer, leaseToken pgtype.UUID) {
	deleteCtx, cancel := context.WithTimeout(ctx, corpusTransferCleanupDeleteTimeout)
	err := r.Storage.DeleteObject(deleteCtx, row.ObjectKey)
	cancel()
	if err != nil {
		backoff := corpusTransferCleanupBackoff(row.CleanupAttempt)
		_, releaseErr := r.Queries.RetryCorpusTransferCleanup(ctx, db.RetryCorpusTransferCleanupParams{
			RetryAfter: pgInterval(backoff), CleanupLastError: err.Error(),
			WorkspaceID: row.WorkspaceID, ID: row.ID, CleanupLeaseToken: leaseToken,
		})
		if releaseErr != nil && ctx.Err() == nil {
			r.logger().Error("corpus transfer cleanup retry persistence failed", "error", releaseErr, "delete_error", err)
		}
		return
	}

	if int(row.CleanupPass) < len(corpusTransferCleanupRedelete) {
		_, err = r.Queries.ScheduleCorpusTransferCleanupPass(ctx, db.ScheduleCorpusTransferCleanupPassParams{
			RetryAfter:  pgInterval(corpusTransferCleanupRedelete[row.CleanupPass]),
			WorkspaceID: row.WorkspaceID, ID: row.ID, CleanupLeaseToken: leaseToken,
		})
	} else {
		err = r.complete(ctx, row, leaseToken)
	}
	if err != nil && ctx.Err() == nil {
		r.logger().Error("corpus transfer cleanup result persistence failed", "error", err)
	}
}

// complete retains evidence for a live workspace but removes the ledger once
// workspace teardown has handed the external object to this reconciler. The
// second orphan delete closes the race where workspace deletion commits
// between the first orphan check and the live-workspace completion update.
func (r *CorpusTransferReconciler) complete(ctx context.Context, row db.CorpusTransfer, leaseToken pgtype.UUID) error {
	deleteParams := db.DeleteOrphanedCorpusTransferAfterCleanupParams{
		WorkspaceID: row.WorkspaceID, ID: row.ID, CleanupLeaseToken: leaseToken,
	}
	if _, err := r.Queries.DeleteOrphanedCorpusTransferAfterCleanup(ctx, deleteParams); err == nil {
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	_, err := r.Queries.CompleteCorpusTransferCleanup(ctx, db.CompleteCorpusTransferCleanupParams{
		WorkspaceID: row.WorkspaceID, ID: row.ID, CleanupLeaseToken: leaseToken,
	})
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	_, err = r.Queries.DeleteOrphanedCorpusTransferAfterCleanup(ctx, deleteParams)
	return err
}

func corpusTransferCleanupBackoff(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 10 {
		shift = 10
	}
	backoff := corpusTransferCleanupBackoffBase * time.Duration(1<<shift)
	if backoff > corpusTransferCleanupBackoffCap {
		return corpusTransferCleanupBackoffCap
	}
	return backoff
}
