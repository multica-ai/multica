-- name: ListComments :many
SELECT * FROM comment
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at ASC;

-- name: ListCommentsPaginated :many
SELECT * FROM comment
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at ASC
LIMIT $3 OFFSET $4;

-- name: ListCommentsSince :many
SELECT * FROM comment
WHERE issue_id = $1 AND workspace_id = $2 AND created_at > $3
ORDER BY created_at ASC;

-- name: ListCommentsSincePaginated :many
SELECT * FROM comment
WHERE issue_id = $1 AND workspace_id = $2 AND created_at > $3
ORDER BY created_at ASC
LIMIT $4 OFFSET $5;

-- name: CountComments :one
SELECT count(*) FROM comment
WHERE issue_id = $1 AND workspace_id = $2;

-- name: GetComment :one
SELECT * FROM comment
WHERE id = $1;

-- name: GetCommentInWorkspace :one
SELECT * FROM comment
WHERE id = $1 AND workspace_id = $2;

-- name: CreateComment :one
INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id)
VALUES ($1, $2, $3, $4, $5, $6, sqlc.narg(parent_id))
RETURNING *;

-- name: UpdateComment :one
UPDATE comment SET
    content = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: HasAgentCommentedSince :one
SELECT EXISTS (
    SELECT 1 FROM comment
    WHERE issue_id = @issue_id
      AND author_type = 'agent'
      AND author_id = @author_id
      AND created_at >= @since
) AS commented;

-- name: HasAgentRepliedInThread :one
-- Returns true if the given agent has posted a reply in the thread rooted at
-- the specified parent comment. Used to detect agent participation in a
-- member-started thread so that follow-up member replies still trigger the agent.
SELECT count(*) > 0 AS has_replied FROM comment
WHERE parent_id = @parent_id AND author_type = 'agent' AND author_id = @agent_id;

-- name: DeleteComment :exec
DELETE FROM comment WHERE id = $1;
