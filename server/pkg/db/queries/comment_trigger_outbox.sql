-- name: CreateCommentTriggerOutbox :exec
INSERT INTO comment_trigger_outbox (
    comment_id, workspace_id, issue_id, actor_type, actor_id,
    originator_user_id, suppress_agent_ids
) VALUES (
    @comment_id, @workspace_id, @issue_id, @actor_type, @actor_id,
    sqlc.narg(originator_user_id), @suppress_agent_ids
)
ON CONFLICT (comment_id) DO NOTHING;

-- name: ExpireExhaustedCommentTriggerOutbox :execrows
UPDATE comment_trigger_outbox
SET state = 'dead', lease_owner = NULL, lease_expires_at = NULL,
    next_attempt_at = NULL,
    last_error = COALESCE(last_error, 'lease expired after retry budget was exhausted'),
    updated_at = now()
WHERE state = 'processing'
  AND lease_expires_at <= now()
  AND attempts >= @max_attempts;

-- name: ClaimCommentTriggerOutboxByComment :one
UPDATE comment_trigger_outbox
SET state = 'processing', attempts = attempts + 1,
    lease_owner = @lease_owner,
    lease_expires_at = now() + make_interval(secs => @lease_seconds::double precision),
    next_attempt_at = NULL, last_error = NULL, updated_at = now()
WHERE comment_id = @comment_id
  AND (
      state = 'pending'
      OR (state = 'processing' AND lease_expires_at <= now())
  )
  AND (next_attempt_at IS NULL OR next_attempt_at <= now())
  AND attempts < @max_attempts
RETURNING *;

-- name: ClaimCommentTriggerOutbox :many
WITH candidates AS (
    SELECT comment_id
    FROM comment_trigger_outbox
    WHERE (
        state = 'pending'
        OR (state = 'processing' AND lease_expires_at <= now())
    )
      AND (next_attempt_at IS NULL OR next_attempt_at <= now())
      AND comment_trigger_outbox.attempts < @max_attempts
    ORDER BY COALESCE(next_attempt_at, created_at), created_at, comment_id
    FOR UPDATE SKIP LOCKED
    LIMIT @row_limit
)
UPDATE comment_trigger_outbox AS outbox
SET state = 'processing', attempts = outbox.attempts + 1,
    lease_owner = @lease_owner,
    lease_expires_at = now() + make_interval(secs => @lease_seconds::double precision),
    next_attempt_at = NULL, last_error = NULL, updated_at = now()
FROM candidates
WHERE outbox.comment_id = candidates.comment_id
RETURNING outbox.*;

-- name: MarkCommentTriggerOutboxDone :execrows
UPDATE comment_trigger_outbox
SET state = 'done', lease_owner = NULL, lease_expires_at = NULL,
    next_attempt_at = NULL, last_error = NULL,
    processed_at = now(), updated_at = now()
WHERE comment_id = @comment_id
  AND state = 'processing'
  AND lease_owner = @lease_owner;

-- name: RetryCommentTriggerOutbox :execrows
UPDATE comment_trigger_outbox
SET state = CASE WHEN attempts >= @max_attempts THEN 'dead' ELSE 'pending' END,
    lease_owner = NULL, lease_expires_at = NULL,
    next_attempt_at = CASE WHEN attempts >= @max_attempts THEN NULL ELSE @next_attempt_at END,
    last_error = @last_error, updated_at = now()
WHERE comment_id = @comment_id
  AND state = 'processing'
  AND lease_owner = @lease_owner;
