-- name: ListWikiPages :many
SELECT id, workspace_id, parent_id, title, slug, position, created_by, updated_by, created_at, updated_at
FROM wiki_page
WHERE workspace_id = $1
ORDER BY parent_id NULLS FIRST, position ASC, created_at ASC;

-- name: GetWikiPage :one
SELECT * FROM wiki_page
WHERE id = $1 AND workspace_id = $2;

-- name: CountWikiPageChildren :one
WITH RECURSIVE descendants AS (
    SELECT id
    FROM wiki_page
    WHERE wiki_page.parent_id = $1 AND wiki_page.workspace_id = $2
  UNION ALL
    SELECT child.id
    FROM wiki_page child
    JOIN descendants parent ON child.parent_id = parent.id
    WHERE child.workspace_id = $2
)
SELECT count(*) FROM descendants;

-- name: CreateWikiPage :one
INSERT INTO wiki_page (workspace_id, parent_id, title, slug, content, position, created_by, updated_by)
VALUES ($1, sqlc.narg('parent_id'), $2, $3, COALESCE(sqlc.narg('content'), ''), $4, sqlc.narg('created_by'), sqlc.narg('updated_by'))
RETURNING *;

-- name: UpdateWikiPage :one
UPDATE wiki_page SET
    title = COALESCE(sqlc.narg('title'), title),
    slug = COALESCE(sqlc.narg('slug'), slug),
    content = COALESCE(sqlc.narg('content'), content),
    position = COALESCE(sqlc.narg('position'), position),
    updated_by = COALESCE(sqlc.narg('updated_by'), updated_by),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteWikiPage :exec
DELETE FROM wiki_page
WHERE id = $1 AND workspace_id = $2;

-- name: GetMaxWikiPagePosition :one
SELECT COALESCE(MAX(position), 0)::float8
FROM wiki_page
WHERE workspace_id = $1
  AND (
    (sqlc.narg('parent_id')::uuid IS NULL AND parent_id IS NULL)
    OR parent_id = sqlc.narg('parent_id')::uuid
  );

-- name: ReorderWikiPage :one
UPDATE wiki_page SET
    position = $3,
    updated_by = sqlc.narg('updated_by'),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;
