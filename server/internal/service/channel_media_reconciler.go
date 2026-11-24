package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The channel-media intent ledger's reconciler settings are fixed constants,
// not configuration: whether they ever need tuning is decided from the
// reconciler metrics.
//
// What the state machine fences, and what the schedule fences:
//
//   - bind vs. delete is fenced by STATE. Once a claim flips a row to
//     'deleting', neither a new upload nor BindMediaRefs can resurrect or
//     attach that key, so the post-claim reference check cannot race a late
//     COMMIT. No timing assumption is involved.
//   - a PUT the client already abandoned cannot be ordered against a DELETE at
//     all: the store may materialize the object after the delete completes.
//     That is fenced by the TOMBSTONE schedule instead of by a single timing
//     bet: after deleting an unreferenced object the row is kept and the
//     object re-deleted on a widening schedule, so a late materialization is
//     reclaimed by a later pass. Escaping requires the object to appear only
//     after the whole schedule has elapsed (see channelMediaTombstoneRedelete).
//     Every pass re-checks the reference first: reclaiming an orphan must
//     never outrank keeping an object something durably reads.
//
// The settle delay therefore carries no correctness weight for bind/commit and
// only sets how long the reconciler waits before treating a pending row as
// abandoned; the tested invariant is settle >> every media/HTTP/DB budget in
// the pipeline (see cmd/server channel media invariant test).
//
// Every deadline below is a DURATION handed to SQL, never a timestamp computed
// here: Postgres derives settle cutoffs, lease expiry, backoff and re-delete
// times from its own now(), so replicas with drifting clocks all read the same
// clock. An app-side timestamp would let a fast replica settle rows whose
// upload is still in flight, or hand out a lease that is already expired.
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
	// channelMediaReconcileSweepLimit bounds how many settle operations one
	// sweep performs (and thus its object-storage work). Rows are claimed one
	// at a time and settled sequentially, so a row released early with a
	// 1-minute backoff can be re-claimed within a long sweep — the limit
	// counts settles, not distinct rows, and just keeps a sweep from running
	// away.
	channelMediaReconcileSweepLimit = 50
	// Backoff for failed object-storage deletes: base << (attempt-1), capped.
	channelMediaReconcileBackoffBase = time.Minute
	channelMediaReconcileBackoffCap  = time.Hour
	// channelMediaReconcileDeleteTimeout bounds ONE object-storage DELETE.
	// The SDK's default HTTP client has no overall request timeout, so a
	// black-holed connection would otherwise wedge the sequential sweep loop
	// forever (single-replica deployments have no other worker to take over).
	// Kept well under the lease so a timed-out delete releases with backoff
	// before any replica could reclaim the row.
	channelMediaReconcileDeleteTimeout = 30 * time.Second
)

// channelMediaTombstoneRedelete is the widening re-delete schedule for a
// tombstoned row, indexed by the row's tombstone_pass. A PUT abandoned by its
// client can still materialize the object after the delete, so each entry
// triggers another idempotent delete; the row is dropped only after the last
// one. Total coverage ~31h, versus the ~seconds a store keeps an abandoned
// request alive.
var channelMediaTombstoneRedelete = []time.Duration{
	15 * time.Minute,
	time.Hour,
	6 * time.Hour,
	24 * time.Hour,
}

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

	// deleteTimeout is overridable for deterministic tests.
	deleteTimeout time.Duration
}

func (r *ChannelMediaReconciler) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

// pgInterval carries a duration to SQL so Postgres computes the deadline
// itself. Every settle/lease/retry decision compares against the database
// clock, so a replica whose own clock has drifted cannot settle a row early,
// hand out a lease that is born expired, or compress the tombstone schedule.
func pgInterval(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
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

// RunOnce settles due ledger rows one at a time: claim a row, settle it, claim
// the next, up to the per-sweep limit. Each claim is taken immediately before
// that row's work, so a row is never left holding a lease it has to wait for —
// its attempt counter and backoff describe deletes that were actually tried.
// All errors are per-row and non-fatal: a row that cannot be settled now backs
// off and is retried on a later sweep (or by another replica after lease
// expiry).
func (r *ChannelMediaReconciler) RunOnce(ctx context.Context) {
	if r.Storage == nil {
		// The wiring only builds a reconciler when a storage backend exists,
		// but a panic here would take down the whole process from a bare
		// goroutine — keep the invariant local. Claiming without a deleter
		// would strand rows in 'deleting' until their lease expires, so do
		// not claim at all.
		r.logger().Error("channel media reconciler: no storage backend; skipping sweep")
		return
	}
	for settles := 0; settles < channelMediaReconcileSweepLimit; settles++ {
		leaseToken := pgtype.UUID{Bytes: uuid.New(), Valid: true}
		row, err := r.Queries.ClaimNextChannelMediaPendingObjectForReconcile(ctx, db.ClaimNextChannelMediaPendingObjectForReconcileParams{
			LeaseToken:  leaseToken,
			Lease:       pgInterval(channelMediaReconcileLease),
			SettleDelay: pgInterval(ChannelMediaReconcileSettleDelay),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			// Shutdown cancels the sweep mid-loop; that is the normal way a
			// sweep ends, not a failure worth waking anyone for.
			if ctx.Err() != nil {
				return
			}
			r.logger().Warn("channel media reconciler: claim failed", "error", err)
			break
		}
		r.settle(ctx, row, leaseToken)
	}
}

// settle runs the row the caller just claimed. The lease it holds only has to
// cover this one row (delete timeout << lease, see the invariant test), so no
// heartbeat is needed: the claim was taken moments ago and every write below
// is lease-token guarded, so a row reclaimed after an expiry ignores the old
// owner's writes rather than being corrupted by them.
func (r *ChannelMediaReconciler) settle(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID) {
	// The reference check runs AFTER the claim flipped the row to 'deleting':
	// from that point BindMediaRefs cannot attach this key, so a negative
	// answer is terminal, not a snapshot race. It runs on tombstone passes too:
	// the object a tombstone re-deletes and the object an attachment reads are
	// the same key, so skipping the check would let a re-delete manufacture the
	// dangling attachment this whole ledger exists to prevent.
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
		if row.State == tombstonedState {
			// Unreachable by design — the object key is per (chat message,
			// resource), so a re-ingest gets its own key and a bind can never
			// attach a key that has left 'pending'. Reaching it means one of
			// those invariants broke, and the safe answer to a broken invariant
			// is to keep the object and raise an anomaly, never to delete a live
			// object and break the attachment reading it.
			r.logger().Error("channel media reconciler: tombstoned object is referenced by an attachment; keeping it",
				"storage_key", row.StorageKey,
				"workspace_id", row.WorkspaceID,
				"chat_message_id", row.ChatMessageID,
				"storage_url", row.StorageUrl,
				"tombstone_pass", row.TombstonePass)
			if r.Metrics != nil {
				r.Metrics.TombstoneReferenced.Inc()
			}
		}
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
	// Unreferenced: hand off to the shared delete + tombstone tail.
	r.settleDeletedObject(ctx, row, leaseToken)
}

// settleDeletedObject deletes the object and then either tombstones the row
// for a later re-delete pass or, once the schedule is exhausted, clears it.
// Shared by the first (unreferenced) settle and every tombstone revisit.
func (r *ChannelMediaReconciler) settleDeletedObject(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID) {
	// The delete runs OUTSIDE any transaction (no DB connection or row lock
	// held across storage I/O), gated by the lease and bounded by its own
	// timeout so one stalled connection cannot wedge the sequential sweep.
	delTimeout := r.deleteTimeout
	if delTimeout == 0 {
		delTimeout = channelMediaReconcileDeleteTimeout
	}
	delCtx, delCancel := context.WithTimeout(ctx, delTimeout)
	err := r.Storage.DeleteObject(delCtx, row.StorageKey)
	delCancel()
	if err != nil {
		if r.Metrics != nil {
			r.Metrics.DeleteFailures.Inc()
		}
		r.release(ctx, row, leaseToken, err)
		return
	}
	// The delete succeeded, but an abandoned PUT may still materialize this
	// object afterwards. Keep the row as a tombstone and re-delete on the
	// widening schedule; drop it only once the schedule is exhausted.
	if next, idx, ok := r.nextTombstonePass(row); ok {
		if r.tombstoneRow(ctx, row, leaseToken, next, idx) {
			msg := "channel media reconciler: deleted unreferenced object; tombstoned for re-delete"
			if row.State == tombstonedState {
				// Re-delete passes are idempotent: usually nothing was there.
				msg = "channel media reconciler: tombstone re-delete pass done"
			}
			r.logger().Info(msg,
				"storage_key", row.StorageKey,
				"workspace_id", row.WorkspaceID,
				"chat_message_id", row.ChatMessageID,
				"attempt", row.Attempt,
				"tombstone_pass", idx,
				"next_redelete_in", next)
			if r.Metrics != nil && row.State != tombstonedState {
				// Count the object once, on the delete that first removed it.
				r.Metrics.ObjectsDeleted.Inc()
			}
		}
		return
	}
	if r.clearRow(ctx, row, leaseToken) {
		r.logger().Info("channel media reconciler: tombstone schedule exhausted; ledger row cleared",
			"storage_key", row.StorageKey,
			"workspace_id", row.WorkspaceID,
			"chat_message_id", row.ChatMessageID,
			"attempt", row.Attempt)
	}
}

const tombstonedState = "tombstoned"

// nextTombstonePass returns the delay before this row's next re-delete pass
// and the schedule position to record, or ok=false when the schedule is
// exhausted and the row may be dropped. A row deleted for the first time
// (state 'deleting') starts at index 0; a tombstone advances from the
// tombstone_pass its last successful delete recorded, so a failed re-delete
// in between (which only writes last_error/attempt) cannot restart the walk.
func (r *ChannelMediaReconciler) nextTombstonePass(row db.ChannelMediaPendingObject) (delay time.Duration, idx int, ok bool) {
	idx = 0
	if row.State == tombstonedState {
		idx = int(row.TombstonePass) + 1
	}
	if idx >= len(channelMediaTombstoneRedelete) {
		return 0, 0, false
	}
	return channelMediaTombstoneRedelete[idx], idx, true
}

func (r *ChannelMediaReconciler) tombstoneRow(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID, next time.Duration, idx int) bool {
	if ctx.Err() != nil {
		// Same as release: a cancelled context cannot carry the write, and
		// shutdown is not a failure. The row stays claimed until its lease
		// expires, and the next pass re-deletes it idempotently.
		return false
	}
	n, err := r.Queries.TombstoneChannelMediaPendingObject(ctx, db.TombstoneChannelMediaPendingObjectParams{
		StorageKey:    row.StorageKey,
		WorkspaceID:   row.WorkspaceID,
		LeaseToken:    leaseToken,
		RedeleteDelay: pgInterval(next),
		TombstonePass: int32(idx),
	})
	if err != nil {
		r.logger().Warn("channel media reconciler: tombstone failed", "storage_key", row.StorageKey, "error", err)
		return false
	}
	return n > 0
}

// release keeps the row in 'deleting' (a bind must still never attach it),
// drops the lease, and backs off the next attempt.
func (r *ChannelMediaReconciler) release(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID, cause error) {
	if ctx.Err() != nil {
		// Shutdown cancelled the settle itself (a DELETE or a query in
		// flight). The backoff write would fail on the same cancelled context,
		// so there is nothing to record and nothing worth waking anyone for —
		// the lease expiry reclaims the row like any other interrupted worker.
		return
	}
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
		StorageKey:  row.StorageKey,
		WorkspaceID: row.WorkspaceID,
		LeaseToken:  leaseToken,
		Backoff:     pgInterval(backoff),
		LastError:   pgtype.Text{String: cause.Error(), Valid: true},
	}); err != nil {
		// The lease expiry reclaims the row regardless.
		r.logger().Warn("channel media reconciler: release failed", "storage_key", row.StorageKey, "error", err)
	}
}

func (r *ChannelMediaReconciler) clearRow(ctx context.Context, row db.ChannelMediaPendingObject, leaseToken pgtype.UUID) bool {
	if ctx.Err() != nil {
		// See release: shutdown mid-settle leaves the row to its lease expiry
		// rather than logging a write that never had a chance to land.
		return false
	}
	n, err := r.Queries.DeleteChannelMediaPendingObject(ctx, db.DeleteChannelMediaPendingObjectParams{
		StorageKey:  row.StorageKey,
		WorkspaceID: row.WorkspaceID,
		LeaseToken:  leaseToken,
	})
	if err != nil {
		r.logger().Warn("channel media reconciler: clear row failed", "storage_key", row.StorageKey, "error", err)
		return false
	}
	return n > 0
}
