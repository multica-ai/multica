package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The channel-media intent ledger's reconciler settings are fixed constants,
// not configuration: whether they ever need tuning is decided from the
// reconciler metrics, and — the load-bearing property — the settle delay
// carries NO correctness weight. Correctness comes from the ledger state
// machine: once a claim flips a row to 'deleting', BindMediaRefs can never
// attach that key, so the post-claim reference check cannot race a late
// COMMIT. The settle delay is only an operational buffer that keeps the
// reconciler from doing wasted work while the normal pipeline is still
// running; the tested invariant is settle >> every media/HTTP/DB budget in
// the pipeline (see cmd/server channel media invariant test).
const (
	// ChannelMediaReconcileSettleDelay is how long a 'pending' row must sit
	// before the reconciler considers it abandoned. Exported for the
	// invariant test.
	ChannelMediaReconcileSettleDelay = 15 * time.Minute
	// channelMediaReconcileSweepInterval paces the reconciler loop.
	channelMediaReconcileSweepInterval = time.Minute
	// channelMediaReconcileLease bounds how long a claimed row stays owned by
	// a worker before another replica may reclaim it (crash recovery).
	channelMediaReconcileLease = 2 * time.Minute
	// channelMediaReconcileBatchLimit bounds one sweep's claim (and thus its
	// object-storage work) — the reconciler's concurrency is one batch,
	// processed sequentially.
	channelMediaReconcileBatchLimit = 50
	// Backoff for failed object-storage deletes: base << (attempt-1), capped.
	channelMediaReconcileBackoffBase = time.Minute
	channelMediaReconcileBackoffCap  = time.Hour
)

// MediaObjectDeleter is the single storage capability the reconciler needs —
// Delete with the error surfaced so failures go to backoff instead of being
// assumed successful. *storage.S3Storage and *storage.LocalStorage satisfy it.
type MediaObjectDeleter interface {
	DeleteObject(ctx context.Context, key string) error
}

// ChannelMediaReconciler settles the media intent ledger: rows written before
// each upload and cleared inside the attachment-bind transaction. Whatever is
// left — upload errors, resolve deadlines, bind failures, ambiguous commits,
// crashes — is claimed here ('pending' → 'deleting' under a lease), checked
// for a durable attachment reference AFTER the claim (race-free: a bind can
// no longer succeed on a claimed key), and then either kept (referenced) or
// deleted from object storage. It runs as an independent worker so
// object-storage latency spikes cannot starve any other sweeper.
type ChannelMediaReconciler struct {
	Queries *db.Queries
	Storage MediaObjectDeleter
	Logger  *slog.Logger
	Metrics *metrics.ChannelMediaReconcilerMetrics

	// now is overridable for deterministic tests.
	now func() time.Time
}

func (r *ChannelMediaReconciler) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

func (r *ChannelMediaReconciler) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// Run loops RunOnce until ctx ends. Started as its own goroutine from
// cmd/server; deliberately not coupled to any other sweeper's cadence.
func (r *ChannelMediaReconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(channelMediaReconcileSweepInterval)
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

// RunOnce claims one batch of due ledger rows and settles each. All errors
// are per-row and non-fatal: a row that cannot be settled now backs off and
// is retried on a later sweep (or by another replica after lease expiry).
func (r *ChannelMediaReconciler) RunOnce(ctx context.Context) {
	leaseToken := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	now := r.clock()
	rows, err := r.Queries.ClaimChannelMediaPendingObjectsForReconcile(ctx, db.ClaimChannelMediaPendingObjectsForReconcileParams{
		LeaseToken:           leaseToken,
		LeaseExpiresAt:       pgtype.Timestamptz{Time: now.Add(channelMediaReconcileLease), Valid: true},
		PendingSettledBefore: pgtype.Timestamptz{Time: now.Add(-ChannelMediaReconcileSettleDelay), Valid: true},
		BatchLimit:           channelMediaReconcileBatchLimit,
	})
	if err != nil {
		r.logger().Warn("channel media reconciler: claim failed", "error", err)
		return
	}
	for _, row := range rows {
		r.settle(ctx, row, leaseToken)
	}
	if r.Metrics != nil {
		if backlog, err := r.Queries.CountChannelMediaPendingObjects(ctx); err == nil {
			r.Metrics.Backlog.Set(float64(backlog))
		}
	}
}

func (r *ChannelMediaReconciler) settle(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID) {
	// The reference check runs AFTER the claim flipped the row to 'deleting':
	// from that point BindMediaRefs cannot attach this key, so a negative
	// answer is terminal, not a snapshot race.
	referenced, err := r.Queries.ChannelMediaObjectIsReferenced(ctx, db.ChannelMediaObjectIsReferencedParams{
		ChatMessageID: row.ChatMessageID,
		WorkspaceID:   row.WorkspaceID,
		StorageUrl:    row.StorageUrl,
	})
	if err != nil {
		r.release(ctx, row, leaseToken, err)
		return
	}
	if referenced {
		// The bind landed (its transaction just lost the pending-row race to
		// an earlier reconciler claim of a slow message, or a redelivered
		// intent outlived the bind). Keep the object, clear the row.
		if r.clearRow(ctx, row, leaseToken) {
			r.logger().Info("channel media reconciler: kept referenced object",
				"storage_key", row.StorageKey,
				"workspace_id", row.WorkspaceID,
				"chat_message_id", row.ChatMessageID)
			if r.Metrics != nil {
				r.Metrics.RowsReferenced.Inc()
			}
		}
		return
	}
	// Unreferenced: delete the object OUTSIDE any transaction (no DB
	// connection or row lock held across storage I/O), gated by the lease.
	if err := r.Storage.DeleteObject(ctx, row.StorageKey); err != nil {
		if r.Metrics != nil {
			r.Metrics.DeleteFailures.Inc()
		}
		r.release(ctx, row, leaseToken, err)
		return
	}
	if r.clearRow(ctx, row, leaseToken) {
		r.logger().Info("channel media reconciler: deleted unreferenced object",
			"storage_key", row.StorageKey,
			"workspace_id", row.WorkspaceID,
			"chat_message_id", row.ChatMessageID,
			"attempt", row.Attempt)
		if r.Metrics != nil {
			r.Metrics.ObjectsDeleted.Inc()
		}
	}
}

// release keeps the row in 'deleting' (a bind must still never attach it),
// drops the lease, and backs off the next attempt.
func (r *ChannelMediaReconciler) release(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID, cause error) {
	backoff := channelMediaReconcileBackoffBase << min(row.Attempt-1, 10)
	if backoff > channelMediaReconcileBackoffCap || backoff <= 0 {
		backoff = channelMediaReconcileBackoffCap
	}
	r.logger().Warn("channel media reconciler: settle failed; backing off",
		"storage_key", row.StorageKey,
		"workspace_id", row.WorkspaceID,
		"attempt", row.Attempt,
		"backoff", backoff,
		"error", cause)
	if err := r.Queries.ReleaseChannelMediaPendingObject(ctx, db.ReleaseChannelMediaPendingObjectParams{
		StorageKey:    row.StorageKey,
		LeaseToken:    leaseToken,
		NextAttemptAt: pgtype.Timestamptz{Time: r.clock().Add(backoff), Valid: true},
		LastError:     pgtype.Text{String: cause.Error(), Valid: true},
	}); err != nil {
		// The lease expiry reclaims the row regardless.
		r.logger().Warn("channel media reconciler: release failed", "storage_key", row.StorageKey, "error", err)
	}
}

func (r *ChannelMediaReconciler) clearRow(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID) bool {
	n, err := r.Queries.DeleteChannelMediaPendingObject(ctx, db.DeleteChannelMediaPendingObjectParams{
		StorageKey: row.StorageKey,
		LeaseToken: leaseToken,
	})
	if err != nil {
		r.logger().Warn("channel media reconciler: clear row failed", "storage_key", row.StorageKey, "error", err)
		return false
	}
	return n > 0
}
