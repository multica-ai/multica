-- name: InsertReference :one
-- UPSERT on (issue_id, object, type, ref_id). On conflict, mutable fields
-- (label, url, metadata) and the timestamps are refreshed; created_by_*
-- stays pinned to the original creator so an UPSERT cannot rewrite history.
INSERT INTO cerebro_issue_reference (
    issue_id, object, type, ref_id, label, url, metadata,
    created_by_type, created_by_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (issue_id, object, type, ref_id) DO UPDATE
SET label      = EXCLUDED.label,
    url        = EXCLUDED.url,
    metadata   = EXCLUDED.metadata,
    updated_at = NOW()
RETURNING *;

-- name: GetReference :one
SELECT * FROM cerebro_issue_reference WHERE id = $1;

-- name: ListReferencesByIssue :many
SELECT * FROM cerebro_issue_reference
WHERE issue_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListIssuesByReference :many
SELECT * FROM cerebro_issue_reference
WHERE object = $1 AND ref_id = $2
ORDER BY created_at ASC, id ASC;

-- name: UpdateReference :one
UPDATE cerebro_issue_reference
SET label      = $2,
    url        = $3,
    metadata   = $4,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteReference :one
DELETE FROM cerebro_issue_reference
WHERE id = $1
RETURNING *;
