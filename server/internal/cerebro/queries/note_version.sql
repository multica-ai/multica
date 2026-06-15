-- Wave 3 / G2 (TECH-3556) — version history for a Note.
-- See migration 9078_cerebro_note_version for the session-coalescing model.

-- name: GetLatestNoteVersion :one
-- The most recent version for a note — used to decide coalesce vs. insert and
-- to compute the next version_no. Returns no rows for a note with no history.
SELECT * FROM cerebro_note_version
WHERE note_id = $1
ORDER BY version_no DESC
LIMIT 1;

-- name: InsertNoteVersion :one
-- Append a fresh version. version_no is computed by the handler (latest + 1).
INSERT INTO cerebro_note_version (
    note_id, version_no, title, body, byte_size, reason, label,
    author_type, author_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: RollForwardNoteVersion :one
-- Coalesce: roll an existing version forward to the latest content (same
-- editing session, same author, within the window). Keeps version_no stable.
UPDATE cerebro_note_version
SET title      = $2,
    body       = $3,
    byte_size  = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListNoteVersions :many
-- History timeline, newest first. Body is included so the client can diff /
-- preview without an extra round-trip per row (notes are small).
SELECT * FROM cerebro_note_version
WHERE note_id = $1
ORDER BY version_no DESC;

-- name: GetNoteVersion :one
SELECT * FROM cerebro_note_version
WHERE note_id = $1 AND id = $2;
