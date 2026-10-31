-- name: GetIssueStageDispatch :one
SELECT * FROM issue_stage_dispatch
WHERE issue_id = $1 AND workspace_id = $2;

-- name: UpsertIssueStageDispatch :one
-- Turns stage dispatch on for a parent issue, or moves it to another squad.
INSERT INTO issue_stage_dispatch (issue_id, workspace_id, squad_id, dispatch_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (issue_id) DO UPDATE
SET squad_id = EXCLUDED.squad_id, dispatch_by = EXCLUDED.dispatch_by, updated_at = now()
RETURNING *;

-- name: DeleteIssueStageDispatch :exec
DELETE FROM issue_stage_dispatch
WHERE issue_id = $1 AND workspace_id = $2;

-- name: DeleteIssueStageDispatchesBySquad :exec
DELETE FROM issue_stage_dispatch
WHERE workspace_id = $1 AND squad_id = $2;

-- name: DeleteIssueStageDispatchesByMember :exec
DELETE FROM issue_stage_dispatch
WHERE workspace_id = $1 AND dispatch_by = $2;

-- name: ListStageDispatchParents :many
-- One page of parents with stage dispatch on, for the periodic sweep. The
-- caller pages with the last issue_id it saw.
SELECT issue_id FROM issue_stage_dispatch
WHERE issue_id > sqlc.arg('after')::uuid
ORDER BY issue_id
LIMIT sqlc.arg('page_size')::int;

-- name: DeleteStageDispatchOfMissingIssue :exec
-- A parent deleted outside the handler paths leaves its row behind; the sweep
-- drops it once the issue is gone.
DELETE FROM issue_stage_dispatch d
WHERE d.issue_id = $1
  AND NOT EXISTS (SELECT 1 FROM issue i WHERE i.id = d.issue_id);
