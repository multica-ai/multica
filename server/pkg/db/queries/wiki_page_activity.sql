-- name: ListWikiPageActivities :many
SELECT * FROM wiki_page_activity
WHERE page_id = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: CreateWikiPageActivity :one
INSERT INTO wiki_page_activity (workspace_id, page_id, actor_id, action, details)
VALUES ($1, $2, sqlc.narg('actor_id'), $3, $4)
RETURNING *;
