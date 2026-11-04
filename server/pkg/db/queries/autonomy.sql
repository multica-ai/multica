-- Squad autonomy levels (spec 4.3). The level is changed through
-- service.ApplyAutonomyChange, which writes the governance entry in the same
-- transaction; the read path uses GetSquadInWorkspace.

-- name: LockSquadForAutonomy :one
-- Serializes autonomy changes for one squad, so the governance entry records the
-- level the change started from.
SELECT * FROM squad WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid FOR UPDATE;

-- name: SetSquadAutonomyLevel :one
UPDATE squad
SET autonomy_level = sqlc.arg('autonomy_level')::integer, updated_at = now()
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: GetSquadAutonomyLevel :one
-- The level alone, for the autopilot admission check and the knowledge publish
-- check, which run on every routine firing and every review.
SELECT autonomy_level FROM squad
WHERE id = sqlc.arg('id')::uuid AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: GetLastAutonomyChange :one
-- The most recent autonomy_changed entry for a squad: its reason and time.
SELECT * FROM governance_event
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND kind = 'autonomy_changed'
  AND subject_type = 'squad'
  AND subject_id = sqlc.arg('subject_id')::uuid
ORDER BY created_at DESC, id DESC
LIMIT 1;
