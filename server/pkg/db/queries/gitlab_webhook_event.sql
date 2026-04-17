-- name: InsertGitlabWebhookEvent :one
-- ON CONFLICT DO NOTHING is the dedupe step: GitLab retries failed
-- deliveries with the same payload, and our own writes generate echoes
-- with the same shape. The unique index on
-- (workspace_id, event_type, object_id, payload_hash) makes a duplicate
-- INSERT a silent no-op. Returning id lets the caller distinguish
-- "fresh" (returned id) from "duplicate" (no row returned).
INSERT INTO gitlab_webhook_event (
    workspace_id, event_type, object_id, gitlab_updated_at, payload_hash, payload
)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, event_type, object_id, payload_hash) DO NOTHING
RETURNING id;

-- name: ClaimNextWebhookEvent :one
-- Pulls the oldest unprocessed event whose backoff window has elapsed.
-- Events that have failed too many times (>= 10) are skipped — they're
-- effectively dead-lettered (still in the table, but never re-claimed).
-- Backoff is exponential in failure_count seconds, capped by the SKIP
-- LOCKED claim itself.
SELECT id, workspace_id, event_type, object_id, gitlab_updated_at, payload, failure_count
FROM gitlab_webhook_event
WHERE processed_at IS NULL
  AND failure_count < 10
  AND (last_attempt_at IS NULL
       OR last_attempt_at < now() - (failure_count * interval '5 seconds'))
ORDER BY received_at
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- name: RecordWebhookEventFailure :exec
-- Increments failure_count + records the error message + bumps last_attempt_at
-- so the backoff filter delays the next retry.
UPDATE gitlab_webhook_event
SET failure_count = failure_count + 1,
    last_attempt_at = now(),
    last_error = $2
WHERE id = $1;

-- name: MarkWebhookEventProcessed :exec
UPDATE gitlab_webhook_event
SET processed_at = now()
WHERE id = $1;

-- name: TouchWorkspaceGitlabLastWebhookReceived :exec
-- Bumps the last-received timestamp for stale-webhook detection.
-- Called by the receiver on every accepted delivery.
UPDATE workspace_gitlab_connection
SET last_webhook_received_at = now()
WHERE workspace_id = $1;

-- name: GetWorkspaceGitlabConnectionByWebhookSecret :one
-- The webhook receiver doesn't have a workspace ID in the URL — only the
-- X-Gitlab-Token header. This query identifies which workspace the
-- delivery is for by matching the secret. The receiver MUST then verify
-- with constant-time comparison (this query just narrows the lookup).
SELECT * FROM workspace_gitlab_connection
WHERE webhook_secret = $1;
