-- name: ReserveIssueCreateRequest :execrows
INSERT INTO issue_create_request (
    workspace_id, actor_type, actor_id, request_key, payload_sha256
) VALUES (
    @workspace_id, @actor_type, @actor_id, @request_key, @payload_sha256
)
ON CONFLICT (workspace_id, actor_type, actor_id, request_key) DO NOTHING;

-- name: GetIssueCreateRequestForUpdate :one
SELECT *
FROM issue_create_request
WHERE workspace_id = @workspace_id
  AND actor_type = @actor_type
  AND actor_id = @actor_id
  AND request_key = @request_key
FOR UPDATE;

-- name: BindIssueCreateRequest :execrows
UPDATE issue_create_request
SET issue_id = @issue_id
WHERE workspace_id = @workspace_id
  AND actor_type = @actor_type
  AND actor_id = @actor_id
  AND request_key = @request_key
  AND issue_id IS NULL
  AND deleted_at IS NULL;

-- name: MarkIssueCreateRequestsDeleted :exec
UPDATE issue_create_request
SET deleted_at = COALESCE(deleted_at, now())
WHERE workspace_id = @workspace_id AND issue_id = @issue_id;
