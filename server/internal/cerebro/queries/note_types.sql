-- TECH-3511: Note types (reusable note templates with recurrence).

-- name: CreateCerebroNoteType :one
INSERT INTO cerebro_note_type (
    workspace_id, name, icon, template_body, recurrence_mode,
    cadence_unit, cadence_count, target_folder_id, enabled, created_by,
    numbering_enabled, next_number
)
VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, sqlc.narg(target_folder_id), $8, $9,
    $10, $11
)
RETURNING *;

-- name: GetCerebroNoteType :one
SELECT * FROM cerebro_note_type WHERE id = $1;

-- name: ListCerebroNoteTypesByWorkspace :many
SELECT * FROM cerebro_note_type
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: UpdateCerebroNoteType :one
UPDATE cerebro_note_type
SET name = $2,
    icon = $3,
    template_body = $4,
    recurrence_mode = $5,
    cadence_unit = $6,
    cadence_count = $7,
    target_folder_id = sqlc.narg(target_folder_id),
    enabled = $8,
    numbering_enabled = $9,
    next_number = $10,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: BumpCerebroNoteTypeNumber :one
-- Atomically assigns the current number to a materialised note and advances the
-- counter. Returns the number that was assigned (the value before the bump).
UPDATE cerebro_note_type
SET next_number = next_number + 1, updated_at = now()
WHERE id = $1
RETURNING next_number - 1 AS assigned_number;

-- name: DeleteCerebroNoteType :exec
DELETE FROM cerebro_note_type WHERE id = $1;

-- name: ListEnabledCerebroNoteTypesForSweep :many
SELECT * FROM cerebro_note_type
WHERE enabled AND cadence_unit <> 'manual';

-- name: SetCerebroNoteTypeRunningDoc :exec
UPDATE cerebro_note_type
SET running_doc_artifact_id = $2, last_period_key = $3, updated_at = now()
WHERE id = $1;

-- name: SetCerebroNoteTypeLastPeriod :exec
UPDATE cerebro_note_type
SET last_period_key = $2, updated_at = now()
WHERE id = $1;

-- name: CreateNoteTypeArtifact :one
INSERT INTO artifact (
    workspace_id, kind, format, title, body,
    author_type, author_id, folder_id, note_type_id, period_key
)
VALUES (
    $1, 'note', 'md', $2, $3,
    'member', $4, sqlc.narg(folder_id), $5, sqlc.narg(period_key)
)
RETURNING *;

-- name: GetNoteTypeArtifactBody :one
SELECT id, workspace_id, body FROM artifact WHERE id = $1;

-- name: UpdateNoteTypeArtifactBody :exec
UPDATE artifact SET body = $2, updated_at = now() WHERE id = $1;

-- name: FindArtifactByNoteTypePeriod :one
SELECT id FROM artifact
WHERE note_type_id = $1 AND period_key = $2;

-- name: GetCerebroNoteTypesFlagForWorkspace :one
-- Workspace-level value of the cerebro_note_types feature flag. pgx.ErrNoRows
-- means the flag has never been set and the default (OFF) applies — the
-- sweeper treats that as "skip this workspace".
SELECT enabled FROM cerebro_feature_flags
WHERE workspace_id = $1
  AND user_id = '00000000-0000-0000-0000-000000000000'
  AND flag_key = 'cerebro_note_types';
