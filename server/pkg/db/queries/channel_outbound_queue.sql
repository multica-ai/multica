-- Durable outbound delivery queue shared by hold-connection channels
-- (migration 265). Every query here is channel-agnostic: channel_type is a
-- parameter or a column, never a literal, so Lark and Slack can adopt this
-- path without a second queue.
--
-- No foreign keys (repo rule): lifecycle cleanup is application-owned via the
-- Fail*/Delete* helpers below and the channel.sql installation teardown paths.

-- =====================
-- Enqueue / claim / terminal updates
-- =====================

-- name: EnqueueChannelOutbound :one
-- Business-key idempotency: (installation_id, source_kind, source_id). A
-- duplicate business result is a no-op (ON CONFLICT DO NOTHING → pgx.ErrNoRows
-- when the caller needs to distinguish fresh insert vs replay).
INSERT INTO channel_outbound_queue (
    installation_id,
    workspace_id,
    channel_type,
    chat_session_id,
    source_kind,
    source_id,
    target_chat_id,
    target_chat_type,
    msg_type,
    payload_version,
    payload
) VALUES (
    $1, $2, $3,
    sqlc.narg('chat_session_id'),
    $4, $5, $6, $7, $8,
    COALESCE(sqlc.narg('payload_version')::smallint, 1::smallint),
    COALESCE(sqlc.narg('payload')::jsonb, '{}'::jsonb)
)
ON CONFLICT (installation_id, source_kind, source_id) DO NOTHING
RETURNING *;

-- name: ClaimChannelOutbound :one
-- Claims one due row for an installation. Status stays queued; lease_token
-- marks in-flight work so a crashed worker can reclaim after lease expiry.
--
-- The EXISTS guards make the claim itself the fence: a row whose installation
-- was revoked, or whose session was unbound/rebound/archived after enqueue, is
-- never handed to a worker at all. The consumer re-checks both before sending
-- because the claim and the send are not in one transaction, but doing it here
-- keeps undeliverable rows from consuming claim attempts in a loop.
WITH candidate AS (
    SELECT q.id
    FROM channel_outbound_queue q
    WHERE q.installation_id = $1
      AND q.status = 'queued'
      AND q.next_attempt_at <= now()
      AND (q.lease_expires_at IS NULL OR q.lease_expires_at <= now())
      AND EXISTS (
            SELECT 1
            FROM channel_installation ci
            WHERE ci.id = q.installation_id
              AND ci.status = 'active'
      )
      AND (
            q.chat_session_id IS NULL
            OR EXISTS (
                SELECT 1
                FROM channel_chat_session_binding b
                JOIN chat_session cs ON cs.id = b.chat_session_id
                WHERE b.chat_session_id = q.chat_session_id
                  AND b.installation_id = q.installation_id
                  AND cs.status = 'active'
            )
      )
    ORDER BY q.next_attempt_at, q.created_at
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
UPDATE channel_outbound_queue AS q
SET lease_token = gen_random_uuid()::text,
    lease_expires_at = $2,
    updated_at = now()
FROM candidate
WHERE q.id = candidate.id
RETURNING q.*;

-- name: DeferClaimedChannelOutbound :one
-- Releases a claim without counting a send attempt. Used when the row was
-- deliverable but must wait (rate-window deferral): deferring through
-- `attempts` would burn the row's retry budget on a condition that is not a
-- failure and would eventually dead-letter a message that was never tried.
UPDATE channel_outbound_queue
SET next_attempt_at = $3,
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = $1
  AND lease_token = $2
  AND status = 'queued'
RETURNING *;

-- name: RetryClaimedChannelOutbound :one
-- Transient send failure: bump attempts, schedule backoff, release lease.
UPDATE channel_outbound_queue
SET attempts = attempts + 1,
    next_attempt_at = $3,
    last_error = $4,
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = $1
  AND lease_token = $2
  AND status = 'queued'
RETURNING *;

-- name: CompleteClaimedChannelOutbound :one
-- Terminal success. payload is cleared because a delivered row's rendered
-- body is dead weight that may quote user content, and the row itself lives
-- on for its retention window as the idempotency tombstone.
UPDATE channel_outbound_queue
SET status = 'sent',
    sent_at = now(),
    payload = '{}'::jsonb,
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = $1
  AND lease_token = $2
  AND status = 'queued'
RETURNING *;

-- name: FailClaimedChannelOutbound :one
-- Terminal failure — this is the dead letter. attempts and last_error stay on
-- the row for operator triage until the retention sweep removes it.
UPDATE channel_outbound_queue
SET status = 'failed',
    payload = '{}'::jsonb,
    last_error = $3,
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE id = $1
  AND lease_token = $2
  AND status = 'queued'
RETURNING *;

-- =====================
-- Lifecycle cleanup
-- =====================

-- name: FailChannelOutboundByInstallation :exec
-- Revoke path: terminal-fail every queued row for an installation, including
-- in-flight leased rows, and strip payload content.
UPDATE channel_outbound_queue
SET status = 'failed',
    payload = '{}'::jsonb,
    last_error = COALESCE(sqlc.narg('last_error'), last_error),
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE installation_id = $1
  AND status = 'queued';

-- name: FailChannelOutboundBySession :exec
-- Archive path: fail unsent queue rows for a session that is no longer a
-- deliverable target.
UPDATE channel_outbound_queue
SET status = 'failed',
    payload = '{}'::jsonb,
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE chat_session_id = $1
  AND status = 'queued';

-- name: FailUndeliverableChannelOutbound :exec
-- Maintenance sweep for rows whose installation or session binding became
-- undeliverable after enqueue. ClaimChannelOutbound already refuses to hand
-- these out, so without this sweep they would sit 'queued' forever and the
-- retention purge — which only removes terminal rows — would never reach them.
UPDATE channel_outbound_queue q
SET status = 'failed',
    payload = '{}'::jsonb,
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE q.status = 'queued'
  AND (
        NOT EXISTS (
            SELECT 1
            FROM channel_installation ci
            WHERE ci.id = q.installation_id
              AND ci.status = 'active'
        )
        OR (
            q.chat_session_id IS NOT NULL
            AND NOT EXISTS (
                SELECT 1
                FROM channel_chat_session_binding b
                JOIN chat_session cs ON cs.id = b.chat_session_id
                WHERE b.chat_session_id = q.chat_session_id
                  AND b.installation_id = q.installation_id
                  AND cs.status = 'active'
            )
        )
  );

-- name: DeleteChannelOutboundBySession :exec
-- Hard delete path (DeleteChatSession): remove queue rows keyed by
-- chat_session_id before the session row itself is deleted.
DELETE FROM channel_outbound_queue q
WHERE q.chat_session_id = $1;

-- name: DeleteChannelOutboundByInstallation :exec
-- Installation hard-delete helper used by replacement/reclaim/runtime paths.
DELETE FROM channel_outbound_queue q
WHERE q.installation_id = $1;

-- name: PurgeSentChannelOutboundQueueBefore :exec
-- Sent queue rows past their retention window. They are kept past delivery
-- only to suppress a re-enqueue of the same business key, so the window need
-- only outlast the reconciler's lookback.
DELETE FROM channel_outbound_queue
WHERE status = 'sent'
  AND updated_at < $1;

-- name: PurgeFailedChannelOutboundQueueBefore :exec
-- Dead-letter rows past their retention window; kept longer than sent rows
-- because they are the record of what failed to deliver and why.
DELETE FROM channel_outbound_queue
WHERE status = 'failed'
  AND updated_at < $1;

-- =====================
-- Reconcile cursor
-- =====================

-- name: ClaimChannelOutboundReconcileState :one
-- Lazily creates this channel's cursor row, then claims it for one scan.
-- FOR UPDATE SKIP LOCKED plus the lease predicate makes the scan
-- single-writer across replicas: a second replica arriving mid-scan updates
-- no row and gets pgx.ErrNoRows, which it treats as "someone else is on it".
WITH ensured AS (
    INSERT INTO channel_outbound_reconcile_state (channel_type, cursor_at)
    VALUES ($1, $2)
    ON CONFLICT (channel_type) DO NOTHING
    RETURNING channel_type
),
candidate AS (
    SELECT s.channel_type
    FROM channel_outbound_reconcile_state s
    WHERE s.channel_type = $1
      AND (s.lease_expires_at IS NULL OR s.lease_expires_at <= now())
    FOR UPDATE SKIP LOCKED
)
UPDATE channel_outbound_reconcile_state AS s
SET lease_token = gen_random_uuid()::text,
    lease_expires_at = now() + interval '30 seconds',
    updated_at = now()
FROM candidate
WHERE s.channel_type = candidate.channel_type
RETURNING s.*;

-- name: ListChannelOutboundReconcileCandidates :many
-- Scans terminal tasks in a fixed [window_start, window_end] slice that still
-- lack a queue row for one of the given source kinds. Stable sort supports
-- page iteration.
--
-- channel_type and source_kinds are parameters so each channel scans only its
-- own bindings and only the source kinds it actually enqueues.
SELECT
    t.id AS task_id,
    t.chat_session_id,
    t.status AS task_status,
    t.completed_at,
    t.failure_reason,
    b.installation_id,
    ci.workspace_id,
    ci.channel_type
FROM agent_task_queue t
JOIN channel_chat_session_binding b
    ON b.chat_session_id = t.chat_session_id
   AND b.channel_type = sqlc.arg('channel_type')
JOIN channel_installation ci
    ON ci.id = b.installation_id
   AND ci.status = 'active'
WHERE t.status IN ('completed', 'failed')
  AND t.completed_at IS NOT NULL
  AND t.chat_session_id IS NOT NULL
  AND t.completed_at > sqlc.arg('window_start')::timestamptz
  AND t.completed_at <= sqlc.arg('window_end')::timestamptz
  AND (
        sqlc.narg('after_completed_at')::timestamptz IS NULL
        OR t.completed_at > sqlc.narg('after_completed_at')::timestamptz
        OR (
            t.completed_at = sqlc.narg('after_completed_at')::timestamptz
            AND t.id > sqlc.narg('after_task_id')::uuid
        )
  )
  AND NOT EXISTS (
        SELECT 1
        FROM channel_outbound_queue q
        WHERE q.installation_id = b.installation_id
          AND q.source_kind = ANY(sqlc.arg('source_kinds')::text[])
          AND q.source_id = t.id::text
  )
ORDER BY t.completed_at, t.id
LIMIT sqlc.arg('limit');

-- name: AdvanceChannelOutboundReconcileState :one
-- Commits the scanned window and releases the lease in one statement. The
-- lease_token predicate stops a replica whose lease already expired — and
-- whose window another replica has since re-scanned — from moving the cursor
-- backwards or past rows it never looked at.
UPDATE channel_outbound_reconcile_state
SET cursor_at = $2,
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE channel_type = $1
  AND lease_token = $3
RETURNING *;

-- name: ReleaseChannelOutboundReconcileState :exec
-- Abandons a scan without advancing: the same window is re-scanned next tick.
UPDATE channel_outbound_reconcile_state
SET lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE channel_type = $1
  AND lease_token = $2;
