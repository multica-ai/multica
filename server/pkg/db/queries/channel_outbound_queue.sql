-- Platform-agnostic outbound delivery queue (WeCom v1 writer; table is
-- channel_type-scoped so other hold-connection platforms can reuse it).
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
-- Releases a claim without counting a send attempt (rate-window deferral).
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
-- Rate window ledger
-- =====================

-- name: LockChannelOutboundRateWindow :exec
-- Serializes minute/hour window checks for one external chat target.
-- The field separator is chr(1), not chr(0): PostgreSQL rejects a NUL byte in
-- any text value, so chr(0) made this lock fail with SQLSTATE 54000 on every
-- outbound send. chr(1) keeps the "cannot occur inside a uuid, a smallint
-- rendering, or a chat id" property that the separator exists for.
SELECT pg_advisory_xact_lock(
    hashtext('wecom_outbound_rate'),
    hashtext(
        (sqlc.arg('installation_id')::uuid)::text
        || chr(1)
        || (sqlc.arg('target_chat_type')::smallint)::text
        || chr(1)
        || sqlc.arg('target_chat_id')::text
    )
);

-- name: CountChannelOutboundAttemptsSince :one
SELECT COUNT(*)::bigint AS attempt_count
FROM channel_outbound_send_attempt
WHERE installation_id = $1
  AND target_chat_type = $2
  AND target_chat_id = $3
  AND attempted_at >= $4;

-- name: RecordChannelOutboundSendAttempt :one
-- Written immediately before handing a frame to writePump; counts toward
-- platform rate limits even when the subsequent write is ambiguous.
INSERT INTO channel_outbound_send_attempt (
    queue_id,
    installation_id,
    workspace_id,
    chat_session_id,
    target_chat_id,
    target_chat_type
) VALUES (
    $1, $2, $3,
    sqlc.narg('chat_session_id'),
    $4, $5
)
RETURNING *;

-- =====================
-- Lifecycle cleanup
-- =====================

-- name: FailChannelOutboundByInstallation :exec
-- Revoke path: terminal-fail every queued row for an installation, including
-- in-flight leased rows, and strip payload secrets.
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
-- Archive path: fail unsent queue rows; send-attempt ledger is retained for
-- platform rate limits (spec §5.3.3).
UPDATE channel_outbound_queue
SET status = 'failed',
    payload = '{}'::jsonb,
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE chat_session_id = $1
  AND status = 'queued';

-- name: FailUndeliverableChannelOutbound :exec
-- Hourly maintenance sweep for rows whose installation or session binding
-- became undeliverable after enqueue.
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
-- Hard delete path (DeleteChatSession): remove queue and send-attempt rows
-- keyed by chat_session_id before the session row itself is deleted.
WITH deleted_send_attempts AS (
    DELETE FROM channel_outbound_send_attempt a
    WHERE a.chat_session_id = $1
)
DELETE FROM channel_outbound_queue q
WHERE q.chat_session_id = $1;

-- name: DeleteChannelOutboundByInstallation :exec
-- Installation hard-delete helper used by replacement/reclaim/runtime paths.
WITH deleted_send_attempts AS (
    DELETE FROM channel_outbound_send_attempt a
    WHERE a.installation_id = $1
)
DELETE FROM channel_outbound_queue q
WHERE q.installation_id = $1;

-- name: PurgeChannelOutboundSendAttemptsBefore :exec
DELETE FROM channel_outbound_send_attempt
WHERE attempted_at < $1;

-- name: PurgeSentChannelOutboundQueueBefore :exec
-- Sent queue rows past their 24h retention window (spec §5.3.3).
DELETE FROM channel_outbound_queue
WHERE status = 'sent'
  AND updated_at < $1;

-- name: PurgeFailedChannelOutboundQueueBefore :exec
-- Failed queue rows past their 7d retention window (spec §5.3.3); kept
-- longer than sent rows to support debugging delivery failures.
DELETE FROM channel_outbound_queue
WHERE status = 'failed'
  AND updated_at < $1;

-- =====================
-- Reconcile cursor (WeCom v1 scanner)
-- =====================

-- name: ClaimChannelOutboundReconcileState :one
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

-- name: ListWecomOutboundReconcileCandidates :many
-- Scans terminal tasks in a fixed [window_start, window_end] slice that still
-- lack a chat_done/task_failed queue row. Stable sort supports page iteration.
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
   AND b.channel_type = 'wecom'
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
          AND q.source_kind IN ('chat_done', 'task_failed')
          AND q.source_id = t.id::text
  )
ORDER BY t.completed_at, t.id
LIMIT sqlc.arg('limit');

-- name: AdvanceChannelOutboundReconcileState :one
UPDATE channel_outbound_reconcile_state
SET cursor_at = $2,
    lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE channel_type = $1
  AND lease_token = $3
RETURNING *;

-- name: ReleaseChannelOutboundReconcileState :exec
UPDATE channel_outbound_reconcile_state
SET lease_token = NULL,
    lease_expires_at = NULL,
    updated_at = now()
WHERE channel_type = $1
  AND lease_token = $2;
