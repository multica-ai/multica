-- name: CreateCollaborationRequest :one
INSERT INTO collaboration_request (
    workspace_id,
    issue_id,
    from_agent_id,
    to_agent_id,
    parent_request_id,
    status,
    mode,
    purpose,
    max_turns,
    depth,
    expires_at,
    metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: UpdateCollaborationRequestQueued :one
UPDATE collaboration_request
SET status = 'queued',
    trigger_comment_id = $2,
    target_task_id = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateCollaborationRequestFailed :one
UPDATE collaboration_request
SET status = 'failed',
    metadata = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: GetCollaborationRequestInWorkspace :one
SELECT * FROM collaboration_request
WHERE id = $1 AND workspace_id = $2;

-- name: ListCollaborationRequestsByIssue :many
SELECT * FROM collaboration_request
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at DESC;

-- name: CountActiveReverseCollaborationRequests :one
SELECT count(*) FROM collaboration_request
WHERE issue_id = $1
  AND workspace_id = $2
  AND from_agent_id = $3
  AND to_agent_id = $4
  AND status IN ('accepted', 'queued')
  AND expires_at > now();

-- name: GetCollaborationRequestDepth :one
SELECT depth FROM collaboration_request
WHERE id = $1 AND workspace_id = $2 AND issue_id = $3;
