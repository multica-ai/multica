-- name: AddReaction :one
INSERT INTO comment_reaction (id, comment_id, workspace_id, actor_type, actor_id, emoji)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (comment_id, actor_type, actor_id, emoji) DO UPDATE SET created_at = comment_reaction.created_at
RETURNING *;

-- name: RemoveReaction :exec
DELETE FROM comment_reaction
WHERE comment_id = ? AND actor_type = ? AND actor_id = ? AND emoji = ?;

-- name: ListReactionsByCommentIDs :many
SELECT * FROM comment_reaction
WHERE comment_id IN (sqlc.slice('comment_ids'))
ORDER BY created_at ASC;
