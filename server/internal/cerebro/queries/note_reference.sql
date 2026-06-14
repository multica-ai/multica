-- name: UpsertNoteReference :one
-- UPSERT on (note_id, object, ref_id). On conflict, mutable fields
-- (type, label, url, metadata) and the timestamps are refreshed; created_by_*
-- stays pinned to the original creator so an UPSERT cannot rewrite history.
INSERT INTO cerebro_note_reference (
    note_id, object, type, ref_id, label, url, metadata,
    created_by_type, created_by_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (note_id, object, ref_id) DO UPDATE
SET type       = EXCLUDED.type,
    label      = EXCLUDED.label,
    url        = EXCLUDED.url,
    metadata   = EXCLUDED.metadata,
    updated_at = NOW()
RETURNING *;

-- name: GetNoteReference :one
SELECT * FROM cerebro_note_reference WHERE id = $1;

-- name: ListNoteReferencesByNote :many
SELECT * FROM cerebro_note_reference
WHERE note_id = $1
ORDER BY created_at ASC, id ASC;

-- name: DeleteNoteReference :one
DELETE FROM cerebro_note_reference
WHERE id = $1
RETURNING *;
