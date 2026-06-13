-- Notes feature (TECH-3421). A Note is an artifact (kind='note') that also has
-- a cerebro_note row carrying owner + visibility + pin state. See migration
-- 9073_cerebro_note. Visibility access rule, applied everywhere a note is read:
--   owner_id = me  OR  visibility='workspace'  OR
--   (visibility='shared' AND a share row exists for me).

-- name: UpsertNote :one
-- Marks an artifact as a Notes-feature note (or updates its note state).
INSERT INTO cerebro_note (artifact_id, owner_id, visibility, pinned, pinned_at, updated_at)
VALUES ($1, $2, $3, $4, CASE WHEN $4 THEN now() ELSE NULL END, now())
ON CONFLICT (artifact_id) DO UPDATE
SET visibility = EXCLUDED.visibility,
    pinned     = EXCLUDED.pinned,
    pinned_at  = CASE WHEN EXCLUDED.pinned THEN COALESCE(cerebro_note.pinned_at, now()) ELSE NULL END,
    updated_at = now()
RETURNING artifact_id, owner_id, visibility, pinned, pinned_at, created_at, updated_at;

-- name: GetNote :one
SELECT artifact_id, owner_id, visibility, pinned, pinned_at, created_at, updated_at
FROM cerebro_note
WHERE artifact_id = $1;

-- name: SetNoteVisibility :exec
UPDATE cerebro_note
SET visibility = $2, updated_at = now()
WHERE artifact_id = $1;

-- name: SetNotePinned :exec
UPDATE cerebro_note
SET pinned = $2,
    pinned_at = CASE WHEN $2 THEN COALESCE(pinned_at, now()) ELSE NULL END,
    updated_at = now()
WHERE artifact_id = $1;

-- name: DeleteNote :exec
DELETE FROM cerebro_note WHERE artifact_id = $1;

-- name: AddNoteShare :exec
INSERT INTO cerebro_note_share (artifact_id, user_id)
VALUES ($1, $2)
ON CONFLICT (artifact_id, user_id) DO NOTHING;

-- name: RemoveNoteShare :exec
DELETE FROM cerebro_note_share WHERE artifact_id = $1 AND user_id = $2;

-- name: ReplaceNoteShares :exec
-- Clears the share list for a note; callers re-add the desired users.
DELETE FROM cerebro_note_share WHERE artifact_id = $1;

-- name: ListNoteShares :many
SELECT user_id, created_at
FROM cerebro_note_share
WHERE artifact_id = $1
ORDER BY created_at;

-- name: CanUserSeeNote :one
-- Returns true when the user is allowed to see the note under the access rule.
SELECT EXISTS (
    SELECT 1 FROM cerebro_note n
    WHERE n.artifact_id = $1
      AND (
        n.owner_id = $2
        OR n.visibility = 'workspace'
        OR (n.visibility = 'shared' AND EXISTS (
            SELECT 1 FROM cerebro_note_share s
            WHERE s.artifact_id = n.artifact_id AND s.user_id = $2))
      )
) AS allowed;

-- name: ListNotesForUser :many
-- The fast Notes list: every note the user may see in a workspace, pinned
-- first (most-recently pinned first), then most-recently-updated.
SELECT a.id, a.workspace_id, a.folder_id, a.title, a.body,
       a.created_at, a.updated_at,
       n.owner_id, n.visibility, n.pinned, n.pinned_at
FROM cerebro_note n
JOIN artifact a ON a.id = n.artifact_id
WHERE a.workspace_id = $1
  AND (
    n.owner_id = $2
    OR n.visibility = 'workspace'
    OR (n.visibility = 'shared' AND EXISTS (
        SELECT 1 FROM cerebro_note_share s
        WHERE s.artifact_id = n.artifact_id AND s.user_id = $2))
  )
ORDER BY n.pinned DESC, n.pinned_at DESC NULLS LAST, a.updated_at DESC
LIMIT $3 OFFSET $4;

-- name: SearchNotesForUser :many
-- Same access rule as ListNotesForUser, filtered by a free-text query over
-- title + body. Private notes only ever match for their owner.
SELECT a.id, a.workspace_id, a.folder_id, a.title, a.body,
       a.created_at, a.updated_at,
       n.owner_id, n.visibility, n.pinned, n.pinned_at
FROM cerebro_note n
JOIN artifact a ON a.id = n.artifact_id
WHERE a.workspace_id = $1
  AND (
    n.owner_id = $2
    OR n.visibility = 'workspace'
    OR (n.visibility = 'shared' AND EXISTS (
        SELECT 1 FROM cerebro_note_share s
        WHERE s.artifact_id = n.artifact_id AND s.user_id = $2))
  )
  AND (
    a.title ILIKE '%' || sqlc.arg('q')::text || '%'
    OR a.body ILIKE '%' || sqlc.arg('q')::text || '%'
  )
ORDER BY n.pinned DESC, n.pinned_at DESC NULLS LAST, a.updated_at DESC
LIMIT $3 OFFSET $4;

-- name: ListRecentNotesForUser :many
-- Compact feed for the Notes box in the dynamic inbox: pinned first, then
-- newest, capped small by the caller via LIMIT.
SELECT a.id, a.workspace_id, a.folder_id, a.title, a.body,
       a.created_at, a.updated_at,
       n.owner_id, n.visibility, n.pinned, n.pinned_at
FROM cerebro_note n
JOIN artifact a ON a.id = n.artifact_id
WHERE a.workspace_id = $1
  AND (
    n.owner_id = $2
    OR n.visibility = 'workspace'
    OR (n.visibility = 'shared' AND EXISTS (
        SELECT 1 FROM cerebro_note_share s
        WHERE s.artifact_id = n.artifact_id AND s.user_id = $2))
  )
ORDER BY n.pinned DESC, n.pinned_at DESC NULLS LAST, a.updated_at DESC
LIMIT $3;
