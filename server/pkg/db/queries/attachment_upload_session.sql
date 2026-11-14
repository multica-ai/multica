-- Presigned direct-upload sessions. See migration 657 for the two-axis state
-- machine (publish_state / cleanup_state).
--
-- The completing transaction is the only writer that publishes an attachment,
-- and it does so under LockAttachmentUploadSessionForComplete, so concurrent
-- completions of one session serialize instead of each creating a row.
--
-- Cleanup writes touch ONLY cleanup_state; publishing writes touch ONLY
-- publish_state. That separation is what keeps a reclaim sweep from making a
-- published upload unreplayable.

-- name: CreateAttachmentUploadSession :one
INSERT INTO attachment_upload_session (
    id, workspace_id, actor_type, actor_id, actor_user_id,
    issue_id, comment_id, chat_session_id, task_id,
    filename, declared_content_type, expected_size,
    staging_key, final_key, expires_at
) VALUES (
    $1, $2, $3, $4, $5,
    sqlc.narg(issue_id), sqlc.narg(comment_id), sqlc.narg(chat_session_id), sqlc.narg(task_id),
    $6, $7, $8,
    $9, $10, $11
)
RETURNING *;

-- name: GetAttachmentUploadSession :one
-- Access-neutral by design, like GetAttachmentByIDOnly: the handler resolves
-- the session id from the URL and compares workspace_id itself, so a session
-- belonging to another workspace is indistinguishable from a missing one.
SELECT * FROM attachment_upload_session
WHERE id = $1;

-- name: LockAttachmentUploadSessionForComplete :one
-- Serializes complete-upload for one session: the second concurrent caller
-- blocks here, then re-reads the row and sees publish_state = 'completed', so
-- exactly one attachment row and one revision bump are ever produced.
SELECT * FROM attachment_upload_session
WHERE id = $1
FOR UPDATE;

-- name: CompleteAttachmentUploadSession :execrows
-- Publishes the session. Guarded on publish_state so a row cannot be published
-- twice even if a caller somehow skips the lock; cleanup_state is untouched, so
-- a concurrent or later sweep cannot undo this.
-- attachment_id records the published attachment for replay.
UPDATE attachment_upload_session
SET publish_state = 'completed',
    attachment_id = sqlc.arg(attachment_id),
    completed_at = now(),
    -- Make the row due for its staging-object reclaim on the next sweep even if
    -- it had already been attempted, without claiming it here (the reconciler
    -- owns cleanup_state and the lease).
    next_attempt_at = now()
WHERE id = sqlc.arg(id)
  AND publish_state = 'pending';

-- name: ClaimNextAttachmentUploadSessionForReconcile :one
-- Short-transaction claim of ONE due session, taken immediately before that
-- session's objects are cleaned, so attempt counts attempts that actually ran.
-- MATERIALIZED pins the bounded candidate set so a nested-loop rescan cannot
-- move the LIMIT window to another row after the update. Mirrors
-- ClaimNextChannelMediaPendingObjectForReconcile.
--
-- Due means:
--   (a) never-published rows ('pending') whose completion deadline (expires_at
--       plus the handler's fixed grace) passed more than the settle delay ago —
--       the session cannot be published any more, so anything in the bucket is
--       garbage. The grace is repeated here as an interval because it must not
--       drift from the handler's value; both are 5 minutes and a test pins it;
--   (b) published rows whose staging object has not been reclaimed yet;
--   (c) 'claimed' rows whose lease expired (a crashed worker);
--   (d) tombstones, because a PUT the client abandoned can still materialize
--       the staging object after a delete returns.
--
-- FOR UPDATE SKIP LOCKED keeps replicas off each other's row; the
-- object-storage DELETE happens outside any transaction, gated by the lease
-- token. No row (ErrNoRows) means nothing is due — the sweep is done.
WITH due AS MATERIALIZED (
    SELECT cand.id FROM attachment_upload_session AS cand
    WHERE cand.next_attempt_at <= now()
      AND (
          (cand.publish_state = 'pending' AND cand.cleanup_state = 'none'
              AND cand.expires_at <= now() - sqlc.arg(upload_grace)::interval - sqlc.arg(settle_delay)::interval)
          OR (cand.publish_state = 'completed' AND cand.cleanup_state = 'none')
          OR (cand.cleanup_state = 'claimed'
              AND (cand.lease_expires_at IS NULL OR cand.lease_expires_at <= now()))
          OR (cand.cleanup_state = 'tombstoned'
              AND (cand.lease_expires_at IS NULL OR cand.lease_expires_at <= now()))
      )
    ORDER BY cand.next_attempt_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
UPDATE attachment_upload_session AS session
SET cleanup_state = CASE
        WHEN session.cleanup_state = 'tombstoned' THEN 'tombstoned'
        ELSE 'claimed'
    END,
    lease_token = sqlc.arg(lease_token),
    lease_expires_at = now() + sqlc.arg(lease_duration)::interval,
    attempt = session.attempt + 1
FROM due
WHERE session.id = due.id
RETURNING session.*;

-- name: ReleaseAttachmentUploadSession :exec
-- Returns a claimed session to the queue with backoff. Used when the object
-- store refused a delete; the row survives so the retry is not lost. Only
-- cleanup columns are written.
UPDATE attachment_upload_session
SET lease_token = NULL,
    lease_expires_at = NULL,
    next_attempt_at = now() + sqlc.arg(backoff)::interval,
    last_error = sqlc.arg(last_error)
WHERE id = sqlc.arg(id)
  AND lease_token = sqlc.arg(lease_token);

-- name: TombstoneAttachmentUploadSession :execrows
-- Records one completed re-delete pass and schedules the next one. The row is
-- kept because a PUT the client abandoned can still materialize the object
-- after a delete returns; only after the last pass is the row dropped.
-- publish_state is deliberately not touched: a published session stays
-- replayable for as long as its row exists.
UPDATE attachment_upload_session
SET cleanup_state = 'tombstoned',
    lease_token = NULL,
    lease_expires_at = NULL,
    attempt = 0,
    tombstone_pass = sqlc.arg(tombstone_pass),
    next_attempt_at = now() + sqlc.arg(backoff)::interval,
    last_error = NULL
WHERE id = sqlc.arg(id)
  AND lease_token = sqlc.arg(lease_token);

-- name: HoldAttachmentUploadSessionForReplay :execrows
-- Keeps a PUBLISHED session's row alive after its objects are reclaimed, until
-- its replay-retention window ends. The row is what makes a repeated
-- complete-upload idempotent: dropping it while a client may still retry turns
-- a successful publish into a 404 for anyone whose response was lost.
--
-- Only cleanup columns move: publish_state stays 'completed' so the replay path
-- still resolves the same attachment, and next_attempt_at is parked at the
-- retention boundary instead of being swept over and over.
UPDATE attachment_upload_session
SET lease_token = NULL,
    lease_expires_at = NULL,
    attempt = 0,
    next_attempt_at = sqlc.arg(hold_until),
    last_error = NULL
WHERE id = sqlc.arg(id)
  AND lease_token = sqlc.arg(lease_token)
  AND publish_state = 'completed';

-- name: DeleteAttachmentUploadSession :execrows
-- Drops a fully settled session. Lease-token guarded like every other
-- reconciler write so a replica whose lease expired cannot delete a row
-- another replica has since claimed.
--
-- A PUBLISHED row is only deletable once its replay-retention window has
-- passed; this predicate is the last line of defence if a caller forgets the
-- hold above, because deleting it early would silently break idempotency.
DELETE FROM attachment_upload_session
WHERE id = sqlc.arg(id)
  AND lease_token = sqlc.arg(lease_token)
  AND (
      publish_state = 'pending'
      OR (completed_at IS NOT NULL AND completed_at <= now() - sqlc.arg(replay_retention)::interval)
  );

-- name: ExpirePendingAttachmentUploadSessionsForWorkspace :execrows
-- Workspace teardown: hand every unsettled session to the reconciler so the
-- objects go with the workspace, instead of deleting rows that still name an
-- object nothing would then reclaim. Only cleanup columns move, so a session
-- that was already published keeps its replay state until the workspace is
-- gone.
UPDATE attachment_upload_session
SET cleanup_state = CASE
        WHEN cleanup_state = 'tombstoned' THEN 'tombstoned'
        ELSE 'claimed'
    END,
    lease_token = NULL,
    lease_expires_at = NULL,
    next_attempt_at = now(),
    last_error = NULL
WHERE workspace_id = $1;
