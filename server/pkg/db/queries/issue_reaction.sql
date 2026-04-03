-- name: AddIssueReaction :one
INSERT INTO issue_reaction (id, issue_id, workspace_id, actor_type, actor_id, emoji)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (issue_id, actor_type, actor_id, emoji) DO UPDATE SET created_at = issue_reaction.created_at
RETURNING *;

-- name: RemoveIssueReaction :exec
DELETE FROM issue_reaction
WHERE issue_id = ? AND actor_type = ? AND actor_id = ? AND emoji = ?;

-- name: ListIssueReactions :many
SELECT * FROM issue_reaction
WHERE issue_id = ?
ORDER BY created_at ASC;
