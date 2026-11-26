-- name: LockWorkspaceForRunFileChanges :one
-- Taken after reading/validating the upload and held through its commit.
-- Conflicts with workspace teardown's FOR UPDATE: either the upload commits
-- before the deletion sweep, or deletion wins and this returns no row.
SELECT id FROM workspace WHERE id = $1 FOR KEY SHARE;

-- name: SaveRunFileChanges :execrows
INSERT INTO run_file_change (workspace_id, task_id, collection_id, manifest, bodies)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (workspace_id, task_id) DO UPDATE
SET manifest = EXCLUDED.manifest, bodies = EXCLUDED.bodies, updated_at = now()
WHERE run_file_change.collection_id = EXCLUDED.collection_id
  AND (run_file_change.manifest->>'status' = 'collecting'
       OR (run_file_change.manifest = EXCLUDED.manifest AND run_file_change.bodies = EXCLUDED.bodies));

-- name: GetRunFileChanges :one
SELECT manifest FROM run_file_change WHERE workspace_id = $1 AND task_id = $2;

-- name: GetRunFileChangeBody :one
SELECT (bodies -> sqlc.arg(file_id)::text)::jsonb AS body FROM run_file_change
WHERE workspace_id = $1 AND task_id = $2;
