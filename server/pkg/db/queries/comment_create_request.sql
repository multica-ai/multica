-- name: ReserveCommentCreateRequest :execrows
INSERT INTO comment_create_request (
    workspace_id, actor_type, actor_id, request_key, payload_sha256
) VALUES (
    @workspace_id, @actor_type, @actor_id, @request_key, @payload_sha256
)
ON CONFLICT (workspace_id, actor_type, actor_id, request_key) DO NOTHING;

-- name: GetCommentCreateRequestForUpdate :one
SELECT * FROM comment_create_request
WHERE workspace_id = @workspace_id
  AND actor_type = @actor_type
  AND actor_id = @actor_id
  AND request_key = @request_key
FOR UPDATE;

-- name: BindCommentCreateRequest :execrows
UPDATE comment_create_request
SET comment_id = @comment_id
WHERE workspace_id = @workspace_id
  AND actor_type = @actor_type
  AND actor_id = @actor_id
  AND request_key = @request_key
  AND comment_id IS NULL
  AND deleted_at IS NULL;

-- name: MarkCommentCreateRequestsDeleted :exec
UPDATE comment_create_request
SET deleted_at = COALESCE(deleted_at, now())
WHERE workspace_id = @workspace_id AND comment_id = @comment_id;

-- name: MarkCommentCreateRequestsDeletedForIssue :exec
-- Preserve request identity before deleting an issue cascades its comments.
-- Without this marker a later retry would see an unbound reservation and could
-- neither replay nor explain that the original object was intentionally gone.
UPDATE comment_create_request AS request
SET deleted_at = COALESCE(request.deleted_at, now())
FROM comment AS source_comment
WHERE request.workspace_id = @workspace_id
  AND source_comment.workspace_id = @workspace_id
  AND source_comment.issue_id = @issue_id
  AND request.comment_id = source_comment.id;
