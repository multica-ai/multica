-- name: CreateMemoryArtifactRevision :one
-- Snapshot the prior state of an artifact into the revision history.
-- Called from UpdateMemoryArtifact / RestoreMemoryArtifact handlers
-- before they apply the new state. The next revision_number is
-- computed inline via subquery so concurrent edits race cleanly under
-- the (memory_artifact_id, revision_number) UNIQUE constraint —
-- losers retry rather than corrupt history.
INSERT INTO memory_artifact_revision (
    memory_artifact_id, workspace_id, revision_number,
    title, content, slug, parent_id,
    anchor_type, anchor_id,
    tags, metadata, always_inject_at_runtime,
    editor_type, editor_id
) VALUES (
    $1, $2,
    COALESCE(
        (SELECT MAX(revision_number) FROM memory_artifact_revision
         WHERE memory_artifact_id = $1),
        0
    ) + 1,
    $3, $4, sqlc.narg('slug'), sqlc.narg('parent_id'),
    sqlc.narg('anchor_type'), sqlc.narg('anchor_id'),
    $5, $6, $7,
    sqlc.narg('editor_type'), sqlc.narg('editor_id')
) RETURNING *;

-- name: ListMemoryArtifactRevisions :many
-- Newest-first metadata-only listing for the history UI / CLI. Excludes
-- content to keep the payload small; full content is fetched per-row
-- via GetMemoryArtifactRevision.
SELECT
    id, memory_artifact_id, workspace_id, revision_number,
    title, slug, anchor_type, anchor_id, tags, always_inject_at_runtime,
    editor_type, editor_id, created_at
FROM memory_artifact_revision
WHERE memory_artifact_id = $1 AND workspace_id = $2
ORDER BY revision_number DESC
LIMIT  sqlc.arg('limit')::int
OFFSET sqlc.arg('offset')::int;

-- name: GetMemoryArtifactRevision :one
-- Full content of a specific revision. (artifact_id, revision_number)
-- is the natural lookup key — and unique per the schema.
SELECT * FROM memory_artifact_revision
WHERE memory_artifact_id = $1
  AND workspace_id = $2
  AND revision_number = $3;
