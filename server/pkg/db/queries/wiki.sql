-- name: ListWikiPages :many
SELECT * FROM wiki_page
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: GetWikiPage :one
SELECT * FROM wiki_page
WHERE id = $1 AND workspace_id = $2;

-- name: GetWikiPageBySlug :one
SELECT * FROM wiki_page
WHERE workspace_id = $1 AND slug = $2;

-- name: CreateWikiPage :one
INSERT INTO wiki_page (
    workspace_id, title, content, slug, created_by_type, created_by_id
) VALUES (
    $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: UpdateWikiPage :one
UPDATE wiki_page SET
    title = COALESCE(sqlc.narg('title'), title),
    content = COALESCE(sqlc.narg('content'), content),
    slug = sqlc.narg('slug'),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteWikiPage :one
DELETE FROM wiki_page
WHERE id = $1 AND workspace_id = $2
RETURNING id;
