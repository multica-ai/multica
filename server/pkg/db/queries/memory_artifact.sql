-- name: ListMemoryArtifacts :many
-- Default list path: workspace-scoped, optional kind / parent / tags
-- filters, archived hidden by default.
--
-- The narg-and-bool pattern (sqlc.arg + sqlc.narg) lets callers pass
-- "no filter" without coercing nil to a specific value. include_archived
-- defaults FALSE so paginated UI doesn't accidentally surface archived
-- rows after a redeploy.
SELECT * FROM memory_artifact
WHERE workspace_id = $1
  AND (sqlc.narg('kind')::text IS NULL OR kind = sqlc.narg('kind'))
  AND (sqlc.narg('parent_id')::uuid IS NULL OR parent_id = sqlc.narg('parent_id'))
  AND (sqlc.arg('include_archived')::bool OR archived_at IS NULL)
ORDER BY created_at DESC
LIMIT  sqlc.arg('limit')::int
OFFSET sqlc.arg('offset')::int;

-- name: CountMemoryArtifacts :one
-- Companion to ListMemoryArtifacts so the UI can paginate without an
-- extra round-trip per page.
SELECT count(*) FROM memory_artifact
WHERE workspace_id = $1
  AND (sqlc.narg('kind')::text IS NULL OR kind = sqlc.narg('kind'))
  AND (sqlc.narg('parent_id')::uuid IS NULL OR parent_id = sqlc.narg('parent_id'))
  AND (sqlc.arg('include_archived')::bool OR archived_at IS NULL);

-- name: GetMemoryArtifact :one
SELECT * FROM memory_artifact
WHERE id = $1 AND workspace_id = $2;

-- name: GetMemoryArtifactBySlug :one
-- Stable URL lookup: /memory/<kind>/<slug>. Returns at most one row
-- thanks to the (workspace_id, kind, slug) uniqueness constraint.
SELECT * FROM memory_artifact
WHERE workspace_id = $1 AND kind = $2 AND slug = $3;

-- name: ListMemoryArtifactsByAnchor :many
-- "Show me everything anchored to issue X" — the query that powers
-- runtime context injection (when an agent claims a task on issue X,
-- the daemon fetches its memory artifacts and embeds them in CLAUDE.md).
SELECT * FROM memory_artifact
WHERE workspace_id  = $1
  AND anchor_type   = $2
  AND anchor_id     = $3
  AND archived_at IS NULL
ORDER BY created_at DESC
LIMIT $4;

-- name: SearchMemoryArtifacts :many
-- Full-text search via the generated tsvector + GIN index.
-- websearch_to_tsquery accepts user-friendly syntax (quotes, OR, -)
-- without erroring on unbalanced quotes the way to_tsquery does.
-- ts_rank_cd weighs cover density so "exact phrase match" outranks
-- "two terms in different parts of a long doc."
SELECT *,
       ts_rank_cd(content_tsv, websearch_to_tsquery('english', $2)) AS rank
FROM memory_artifact
WHERE workspace_id = $1
  AND archived_at IS NULL
  AND content_tsv @@ websearch_to_tsquery('english', $2)
  AND (sqlc.narg('kind')::text IS NULL OR kind = sqlc.narg('kind'))
ORDER BY rank DESC, created_at DESC
LIMIT  sqlc.arg('limit')::int
OFFSET sqlc.arg('offset')::int;

-- name: CreateMemoryArtifact :one
-- Author validation (member vs agent existence) happens in the service
-- layer; the SQL just records what it's told. Slug optional — pass an
-- empty TEXT (validated as null in service) for "no slug."
INSERT INTO memory_artifact (
    workspace_id, kind, parent_id,
    title, content, slug,
    anchor_type, anchor_id,
    author_type, author_id,
    tags, metadata
) VALUES (
    $1, $2, sqlc.narg('parent_id'),
    $3, $4, sqlc.narg('slug'),
    sqlc.narg('anchor_type'), sqlc.narg('anchor_id'),
    $5, $6,
    $7, $8
) RETURNING *;

-- name: UpdateMemoryArtifact :one
-- Partial update via narg COALESCE. Only fields the caller passes get
-- updated. Tags and metadata pass through directly because they're
-- already nullable JSONB / array — narg semantics handled by sqlc.
UPDATE memory_artifact SET
    title       = COALESCE(sqlc.narg('title'), title),
    content     = COALESCE(sqlc.narg('content'), content),
    slug        = sqlc.narg('slug'),
    parent_id   = sqlc.narg('parent_id'),
    anchor_type = sqlc.narg('anchor_type'),
    anchor_id   = sqlc.narg('anchor_id'),
    tags        = COALESCE(sqlc.narg('tags')::text[], tags),
    metadata    = COALESCE(sqlc.narg('metadata')::jsonb, metadata),
    updated_at  = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ArchiveMemoryArtifact :one
-- Soft-delete: stamps archived_at + archived_by. Idempotent — re-archive
-- is a no-op the handler converts to 409.
UPDATE memory_artifact
SET archived_at = now(),
    archived_by = $3,
    updated_at  = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: RestoreMemoryArtifact :one
UPDATE memory_artifact
SET archived_at = NULL,
    archived_by = NULL,
    updated_at  = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteMemoryArtifact :exec
-- Hard delete. Available but not the default — UI surfaces archive
-- as the primary action; delete is reserved for admins.
DELETE FROM memory_artifact
WHERE id = $1 AND workspace_id = $2;
