-- name: CreateIssueSourceContext :one
INSERT INTO issue_source_context (
    id, workspace_id, issue_id, origin_task_id, source_issue_id,
    anchor_comment_id, captured_by_user_id, snapshot_version, snapshot,
    capture_digest, state, captured_at, attached_at
) VALUES (
    sqlc.arg(id), sqlc.arg(workspace_id), sqlc.narg(issue_id),
    sqlc.narg(origin_task_id), sqlc.arg(source_issue_id),
    sqlc.arg(anchor_comment_id), sqlc.arg(captured_by_user_id),
    sqlc.arg(snapshot_version), sqlc.arg(snapshot), sqlc.arg(capture_digest),
    sqlc.arg(state), sqlc.arg(captured_at), sqlc.narg(attached_at)
)
RETURNING *;

-- name: GetIssueSourceContextByIssue :one
SELECT * FROM issue_source_context
WHERE workspace_id = sqlc.arg(workspace_id)
  AND issue_id = sqlc.arg(issue_id)
  AND state = 'attached';

-- name: GetIssueSourceContextByID :one
SELECT * FROM issue_source_context
WHERE workspace_id = sqlc.arg(workspace_id)
  AND id = sqlc.arg(id);

-- name: GetPendingIssueSourceContextByOriginTask :one
SELECT * FROM issue_source_context
WHERE workspace_id = sqlc.arg(workspace_id)
  AND origin_task_id = sqlc.arg(origin_task_id)
  AND state = 'pending'
FOR UPDATE;

-- name: AttachIssueSourceContext :one
UPDATE issue_source_context
SET issue_id = sqlc.arg(issue_id),
    state = 'attached',
    attached_at = now()
WHERE workspace_id = sqlc.arg(workspace_id)
  AND id = sqlc.arg(id)
  AND state = 'pending'
  AND origin_task_id = sqlc.arg(origin_task_id)
RETURNING *;

-- name: TransferPendingIssueSourceContextTask :one
UPDATE issue_source_context
SET origin_task_id = sqlc.arg(new_task_id)
WHERE workspace_id = sqlc.arg(workspace_id)
  AND id = sqlc.arg(id)
  AND state = 'pending'
  AND origin_task_id = sqlc.arg(old_task_id)
RETURNING *;

-- name: DeleteIssueSourceContextByIssue :one
DELETE FROM issue_source_context
WHERE workspace_id = sqlc.arg(workspace_id)
  AND issue_id = sqlc.arg(issue_id)
  AND state = 'attached'
RETURNING *;

-- name: ListIssueSourceContextsForWorkspace :many
SELECT * FROM issue_source_context
WHERE workspace_id = sqlc.arg(workspace_id)
ORDER BY captured_at ASC;

-- name: DeleteIssueSourceContextsForWorkspace :many
DELETE FROM issue_source_context
WHERE workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: ListExpiredPendingSourceContextsForCleanup :many
SELECT isc.*
FROM issue_source_context isc
JOIN agent_task_queue task ON task.id = isc.origin_task_id
WHERE isc.state = 'pending'
  AND isc.captured_at < now() - interval '30 days'
  AND task.status IN ('completed', 'failed', 'cancelled')
ORDER BY isc.captured_at, isc.id
LIMIT sqlc.arg(row_limit)
FOR UPDATE OF isc SKIP LOCKED;

-- name: AbandonIssueSourceContext :execrows
UPDATE issue_source_context
SET state = 'abandoned', origin_task_id = NULL
WHERE id = sqlc.arg(id)
  AND workspace_id = sqlc.arg(workspace_id)
  AND state = 'pending';

-- name: ListAbandonedSourceContextsForCleanup :many
SELECT isc.*
FROM issue_source_context isc
WHERE isc.state = 'abandoned'
ORDER BY isc.captured_at, isc.id
LIMIT sqlc.arg(row_limit);

-- name: DeleteAbandonedIssueSourceContext :execrows
DELETE FROM issue_source_context
WHERE workspace_id = sqlc.arg(workspace_id)
  AND id = sqlc.arg(id)
  AND state = 'abandoned';

-- name: RecordSourceContextObjectIntent :one
WITH locked_workspace AS MATERIALIZED (
    SELECT id
    FROM workspace
    WHERE id = sqlc.arg(workspace_id)
    FOR KEY SHARE
)
INSERT INTO issue_source_context_object_intent (
    storage_key, workspace_id, source_context_id, attachment_id, object_url
)
SELECT sqlc.arg(storage_key), locked_workspace.id, sqlc.arg(source_context_id),
       sqlc.arg(attachment_id), sqlc.arg(object_url)
FROM locked_workspace
RETURNING *;

-- name: RecordSourceContextDeletionObjectIntent :one
INSERT INTO issue_source_context_object_intent (
    storage_key, workspace_id, source_context_id, attachment_id, object_url
) VALUES (
    sqlc.arg(storage_key), sqlc.arg(workspace_id), sqlc.arg(source_context_id),
    sqlc.arg(attachment_id), sqlc.arg(object_url)
)
RETURNING *;

-- name: DeletePendingSourceContextObjectIntents :many
DELETE FROM issue_source_context_object_intent
WHERE workspace_id = sqlc.arg(workspace_id)
  AND source_context_id = sqlc.arg(source_context_id)
  AND state = 'pending'
RETURNING storage_key;

-- name: ClaimSourceContextObjectIntentForCleanup :one
UPDATE issue_source_context_object_intent AS intent
SET state = 'deleting',
    lease_token = sqlc.arg(lease_token),
    lease_expires_at = now() + interval '2 minutes'
FROM (
    SELECT candidate.storage_key
    FROM issue_source_context_object_intent candidate
    WHERE candidate.next_attempt_at <= now()
      AND (
        (candidate.state = 'pending' AND candidate.created_at <= now() - interval '1 hour')
        OR (candidate.state = 'deleting' AND (candidate.lease_expires_at IS NULL OR candidate.lease_expires_at <= now()))
      )
    ORDER BY candidate.next_attempt_at, candidate.storage_key
    LIMIT 1
    FOR UPDATE SKIP LOCKED
) due
WHERE intent.storage_key = due.storage_key
RETURNING intent.*;

-- name: SourceContextObjectIntentIsReferenced :one
SELECT EXISTS (
    SELECT 1 FROM attachment
    WHERE workspace_id = sqlc.arg(workspace_id)
      AND source_context_id = sqlc.arg(source_context_id)
      AND id = sqlc.arg(attachment_id)
      AND url = sqlc.arg(object_url)
) AS referenced;

-- name: ReleaseSourceContextObjectIntent :execrows
UPDATE issue_source_context_object_intent
SET lease_token = NULL,
    lease_expires_at = NULL,
    next_attempt_at = now() + interval '5 minutes',
    last_error = sqlc.arg(last_error)
WHERE storage_key = sqlc.arg(storage_key)
  AND workspace_id = sqlc.arg(workspace_id)
  AND lease_token = sqlc.arg(lease_token);

-- name: DeleteClaimedSourceContextObjectIntent :execrows
DELETE FROM issue_source_context_object_intent
WHERE storage_key = sqlc.arg(storage_key)
  AND workspace_id = sqlc.arg(workspace_id)
  AND lease_token = sqlc.arg(lease_token);

-- name: ListSourceContextObjectIntentURLsByWorkspace :many
SELECT object_url
FROM issue_source_context_object_intent
WHERE workspace_id = sqlc.arg(workspace_id)
ORDER BY storage_key;

-- name: DeleteSourceContextObjectIntentsByWorkspace :exec
DELETE FROM issue_source_context_object_intent
WHERE workspace_id = sqlc.arg(workspace_id);
