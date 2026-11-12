package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The presigned-upload session reconciler reclaims object-storage bytes that
// outlive their session: the staging object of an upload that was published
// (the attachment reads the final key), never completed, or never even started.
//
// It is the same shape as the channel-media intent ledger's reconciler
// (channel_media_reconciler.go) and exists for the same reason: a presigned PUT
// is issued to a client that the server does not control, so "the object will
// not appear" is not a fact the server can record. The safe order is therefore
// to delete first and keep a tombstone, re-deleting on a widening schedule for
// a PUT that was abandoned mid-flight and materializes after the delete returns.
//
// What the state machine fences, and what the schedule fences:
//
//   - publish vs. delete is fenced by STATE and by the ROW LOCK. complete-upload
//     publishes under the session's FOR UPDATE, and the claim below takes the
//     same row FOR UPDATE SKIP LOCKED, so a session cannot be claimed while its
//     staged object is being finalized. Conversely, a claimed session is
//     'deleting' and complete-upload refuses it — it can never copy a staged
//     object that this reconciler is deleting.
//   - a PUT the client already abandoned cannot be ordered against a DELETE at
//     all: the store may materialize the object after the delete completes.
//     That is fenced by the TOMBSTONE schedule instead of by a timing bet.
//
// Every deadline is a DURATION handed to SQL, never a timestamp computed here,
// so replicas with drifting clocks all act on the database's now().
const (
	// AttachmentUploadReplayRetention is how long a PUBLISHED session's row is
	// kept after publication so a repeated complete-upload still returns the
	// same attachment. It exists because the one client that needs it is the one
	// whose response was lost: dropping the row the moment its objects are
	// reclaimed turns a successful publish into a 404 on retry. It only bounds
	// the replay window; it never affects the attachment, which is a normal row.
	AttachmentUploadReplayRetention = 24 * time.Hour

	// AttachmentUploadCompleteGrace mirrors the handler's completion grace. It
	// is declared here as well because this package owns the sweep, and the
	// sweep must not reclaim a session's objects while completion is still
	// accepted. A test pins the two to the same value.
	AttachmentUploadCompleteGrace = 5 * time.Minute

	// AttachmentUploadReconcileSettleDelay is how long past a session's expiry
	// the reconciler waits before reclaiming an incomplete upload's staging
	// object. The presigned URL is dead at expires_at; the delay covers a PUT
	// that was still streaming when it expired. Exported for the invariant test.
	AttachmentUploadReconcileSettleDelay = 15 * time.Minute

	// attachmentUploadReconcileSweepInterval paces the loop.
	attachmentUploadReconcileSweepInterval = time.Minute

	// attachmentUploadReconcileLease bounds how long a claimed session stays
	// owned by one worker before another replica may reclaim it (crash
	// recovery). It is well above the per-sweep delete timeout.
	attachmentUploadReconcileLease = 2 * time.Minute

	// attachmentUploadReconcileSweepLimit bounds how many settlements one sweep
	// performs, and thus its object-storage work.
	attachmentUploadReconcileSweepLimit = 50

	// attachmentUploadReconcileDeleteTimeout bounds ONE object-storage DELETE,
	// so a black-holed connection cannot wedge the sequential sweep.
	attachmentUploadReconcileDeleteTimeout = 30 * time.Second

	// Backoff for failed object-storage deletes: base << (attempt-1), capped.
	attachmentUploadReconcileBackoffBase = time.Minute
	attachmentUploadReconcileBackoffCap  = time.Hour
)

const (
	// publishedState is the publish_state of a session whose attachment row
	// exists. It never reverts: an idempotent replay depends on it, and the
	// cleanup worker must not be able to make a published upload unreplayable.
	publishedState = "completed"
	// cleanupTombstoned is the cleanup_state of a row deleted once and waiting
	// for a re-delete pass. (The channel-media reconciler next door uses the
	// bare name tombstonedState for its own single-axis table.)
	cleanupTombstoned = "tombstoned"
)

// attachmentUploadTombstoneRedelete is the widening re-delete schedule for a
// tombstoned session, indexed by tombstone_pass. A PUT abandoned by its client
// can still materialize the object after the delete, so each entry triggers
// another idempotent delete; the row is dropped only after the last one. Total
// coverage ~31h, versus the seconds a store keeps an abandoned request alive.
var attachmentUploadTombstoneRedelete = []time.Duration{
	15 * time.Minute,
	time.Hour,
	6 * time.Hour,
	24 * time.Hour,
}

// AttachmentUploadReconciler reclaims staged and orphaned upload objects. It
// runs as an independent worker from cmd/server, so object-storage latency
// cannot starve any other sweeper's cadence.
type AttachmentUploadReconciler struct {
	Queries *db.Queries
	Storage MediaObjectDeleter
	Logger  *slog.Logger

	// sweepInterval and deleteTimeout are overridable for deterministic tests.
	sweepInterval time.Duration
	deleteTimeout time.Duration
}

func (r *AttachmentUploadReconciler) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}

func (r *AttachmentUploadReconciler) interval() time.Duration {
	if r.sweepInterval > 0 {
		return r.sweepInterval
	}
	return attachmentUploadReconcileSweepInterval
}

func (r *AttachmentUploadReconciler) sweepDeleteTimeout() time.Duration {
	if r.deleteTimeout > 0 {
		return r.deleteTimeout
	}
	return attachmentUploadReconcileDeleteTimeout
}

// Run loops RunOnce until ctx ends.
func (r *AttachmentUploadReconciler) Run(ctx context.Context) {
	ticker := time.NewTicker(r.interval())
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

// RunOnce settles due sessions one at a time: claim, settle, claim the next.
// Each claim is taken immediately before that session's work, so its attempt
// counter describes deletes that were actually tried. All errors are per-row
// and non-fatal: a session that cannot be settled now backs off to a later
// sweep (or to another replica after its lease expires).
func (r *AttachmentUploadReconciler) RunOnce(ctx context.Context) {
	if r.Storage == nil {
		// Claiming without a deleter would strand rows in 'deleting' until
		// their leases expire, so do not claim at all.
		r.logger().Error("attachment upload reconciler: no storage backend; skipping sweep")
		return
	}
	for settles := 0; settles < attachmentUploadReconcileSweepLimit; settles++ {
		leaseToken := pgtype.UUID{Bytes: uuid.New(), Valid: true}
		session, err := r.Queries.ClaimNextAttachmentUploadSessionForReconcile(ctx, db.ClaimNextAttachmentUploadSessionForReconcileParams{
			LeaseToken:    leaseToken,
			LeaseDuration: pgInterval(attachmentUploadReconcileLease),
			// The session's own completion grace, so the sweep never reclaims
			// objects while completion is still allowed. Kept equal to the
			// handler's completeUploadGrace by TestUploadGraceMatchesHandler.
			UploadGrace: pgInterval(AttachmentUploadCompleteGrace),
			SettleDelay: pgInterval(AttachmentUploadReconcileSettleDelay),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			// Shutdown cancels the sweep mid-loop; that is how a sweep normally
			// ends, not a failure worth waking anyone for.
			if ctx.Err() != nil {
				return
			}
			r.logger().Warn("attachment upload reconciler: claim failed", "error", err)
			break
		}
		r.settle(ctx, session, leaseToken)
	}
}

// settle reclaims one claimed session's objects.
func (r *AttachmentUploadReconciler) settle(ctx context.Context, session db.AttachmentUploadSession, leaseToken pgtype.UUID) {
	// A published session's attachment reads final_key, so only the staging
	// object is garbage. A session that was never published has no attachment,
	// so both keys are candidates — a final key can only exist for a session
	// whose copy landed but whose transaction did not, and leaving it would be
	// an unreferenced object nothing else could name.
	//
	// publish_state decides this, never cleanup_state: cleanup_state moves as
	// the reclaim progresses, and treating a swept-but-published session as
	// unpublished would delete the object its attachment reads.
	keys := []string{session.StagingKey}
	if session.PublishState != publishedState {
		keys = append(keys, session.FinalKey)
	}

	for _, key := range keys {
		if key == "" {
			continue
		}
		// The delete runs OUTSIDE any transaction (no connection or row lock
		// held across storage I/O), gated by the lease and bounded by its own
		// timeout.
		delCtx, delCancel := context.WithTimeout(ctx, r.sweepDeleteTimeout())
		err := r.Storage.DeleteObject(delCtx, key)
		delCancel()
		if err != nil {
			r.release(ctx, session, leaseToken, err)
			return
		}
	}

	// The deletes succeeded, but an abandoned PUT may still materialize the
	// staging object. Keep the session as a tombstone and re-delete on the
	// widening schedule; drop it only once the schedule is exhausted.
	if next, idx, ok := r.nextTombstonePass(session); ok {
		r.tombstone(ctx, session, leaseToken, next, idx)
		return
	}

	// A published session keeps its row until the replay-retention window ends:
	// its attachment is already safe, but the row is the only thing that can
	// answer a retried complete-upload with the same attachment.
	if session.PublishState == publishedState && session.CompletedAt.Valid {
		if holdUntil := session.CompletedAt.Time.Add(AttachmentUploadReplayRetention); time.Now().Before(holdUntil) {
			r.hold(ctx, session, leaseToken, holdUntil)
			return
		}
	}
	r.clear(ctx, session, leaseToken)
}

// nextTombstonePass returns the delay before this session's next re-delete pass
// and the schedule position to record, or ok=false when the schedule is
// exhausted and the row may be dropped. A session deleted for the first time
// (cleanup_state 'claimed') starts at index 0; a tombstone advances from the
// tombstone_pass its last successful delete recorded, so a failed re-delete in
// between (which only writes last_error) cannot restart the walk.
func (r *AttachmentUploadReconciler) nextTombstonePass(session db.AttachmentUploadSession) (delay time.Duration, idx int, ok bool) {
	idx = 0
	if session.CleanupState == cleanupTombstoned {
		idx = int(session.TombstonePass) + 1
	}
	if idx >= len(attachmentUploadTombstoneRedelete) {
		return 0, 0, false
	}
	return attachmentUploadTombstoneRedelete[idx], idx, true
}

func (r *AttachmentUploadReconciler) tombstone(ctx context.Context, session db.AttachmentUploadSession, leaseToken pgtype.UUID, next time.Duration, idx int) {
	if ctx.Err() != nil {
		// A cancelled context cannot carry the write, and shutdown is not a
		// failure. The row stays claimed until its lease expires and the next
		// pass re-deletes idempotently.
		return
	}
	n, err := r.Queries.TombstoneAttachmentUploadSession(ctx, db.TombstoneAttachmentUploadSessionParams{
		Backoff:       pgInterval(next),
		TombstonePass: int32(idx),
		ID:            session.ID,
		LeaseToken:    leaseToken,
	})
	if err != nil {
		r.logger().Warn("attachment upload reconciler: tombstone failed", "session_id", util.UUIDToString(session.ID), "error", err)
		return
	}
	if n > 0 {
		r.logger().Info("attachment upload reconciler: deleted upload objects; tombstoned for re-delete",
			"session_id", util.UUIDToString(session.ID),
			"workspace_id", util.UUIDToString(session.WorkspaceID),
			"tombstone_pass", idx,
			"next_redelete_in", next)
	}
}

// release keeps the row in its claimed state, drops the lease and backs off the
// next attempt, so a failed delete is retried rather than forgotten.
func (r *AttachmentUploadReconciler) release(ctx context.Context, session db.AttachmentUploadSession, leaseToken pgtype.UUID, cause error) {
	if ctx.Err() != nil {
		// Shutdown cancelled the settle itself; the backoff write would fail on
		// the same cancelled context and the lease expiry covers the retry.
		return
	}
	backoff := attachmentUploadBackoff(session.Attempt + 1)
	if err := r.Queries.ReleaseAttachmentUploadSession(ctx, db.ReleaseAttachmentUploadSessionParams{
		Backoff:    pgInterval(backoff),
		LastError:  pgtype.Text{String: cause.Error(), Valid: true},
		ID:         session.ID,
		LeaseToken: leaseToken,
	}); err != nil {
		r.logger().Warn("attachment upload reconciler: release failed", "session_id", util.UUIDToString(session.ID), "error", err)
		return
	}
	r.logger().Warn("attachment upload reconciler: delete failed; backing off",
		"session_id", util.UUIDToString(session.ID),
		"attempt", session.Attempt+1,
		"retry_in", backoff,
		"error", cause)
}

// hold parks a published session until its replay window ends. Its objects are
// already gone; only the row remains, so a retry can still resolve the same
// attachment.
func (r *AttachmentUploadReconciler) hold(ctx context.Context, session db.AttachmentUploadSession, leaseToken pgtype.UUID, until time.Time) {
	if ctx.Err() != nil {
		return
	}
	n, err := r.Queries.HoldAttachmentUploadSessionForReplay(ctx, db.HoldAttachmentUploadSessionForReplayParams{
		HoldUntil:  pgtype.Timestamptz{Time: until, Valid: true},
		ID:         session.ID,
		LeaseToken: leaseToken,
	})
	if err != nil {
		r.logger().Warn("attachment upload reconciler: hold for replay failed",
			"session_id", util.UUIDToString(session.ID), "error", err)
		return
	}
	if n > 0 {
		r.logger().Info("attachment upload reconciler: objects reclaimed; session held for replay",
			"session_id", util.UUIDToString(session.ID),
			"until", until.Format(time.RFC3339))
	}
}

func (r *AttachmentUploadReconciler) clear(ctx context.Context, session db.AttachmentUploadSession, leaseToken pgtype.UUID) {
	if ctx.Err() != nil {
		return
	}
	n, err := r.Queries.DeleteAttachmentUploadSession(ctx, db.DeleteAttachmentUploadSessionParams{
		ID:              session.ID,
		LeaseToken:      leaseToken,
		ReplayRetention: pgInterval(AttachmentUploadReplayRetention),
	})
	if err != nil {
		r.logger().Warn("attachment upload reconciler: clear failed", "session_id", util.UUIDToString(session.ID), "error", err)
		return
	}
	if n > 0 {
		r.logger().Info("attachment upload reconciler: tombstone schedule exhausted; session cleared",
			"session_id", util.UUIDToString(session.ID),
			"workspace_id", util.UUIDToString(session.WorkspaceID))
	}
}

// attachmentUploadBackoff is exponential from the base, capped; attempt is
// 1-indexed.
func attachmentUploadBackoff(attempt int32) time.Duration {
	backoff := attachmentUploadReconcileBackoffBase
	for i := int32(1); i < attempt; i++ {
		backoff *= 2
		if backoff >= attachmentUploadReconcileBackoffCap {
			return attachmentUploadReconcileBackoffCap
		}
	}
	return backoff
}
