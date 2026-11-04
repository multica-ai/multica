-- Organisation changes (spec 4.6): the application record and its probation,
-- and the few writes the eight change types need that no other query makes.

-- name: InsertOrgChangeApplication :one
INSERT INTO org_change_application (
    id, workspace_id, proposal_id, change_type, subject_type, subject_id,
    before_state, after_state, applied_by_type, applied_by_id,
    probation_ends_at, before_scorecard_id
) VALUES (
    @id, @workspace_id, @proposal_id, @change_type, @subject_type, @subject_id,
    @before_state, @after_state, @applied_by_type, sqlc.narg('applied_by_id'),
    sqlc.narg('probation_ends_at')::timestamptz, sqlc.narg('before_scorecard_id')
)
RETURNING *;

-- name: GetOrgChangeApplicationByProposal :one
SELECT * FROM org_change_application
WHERE workspace_id = @workspace_id AND proposal_id = @proposal_id;

-- name: MarkOrgChangeApplicationReverted :one
UPDATE org_change_application SET
    reverted_at = now(),
    reverted_by_type = @reverted_by_type,
    reverted_by_id = sqlc.narg('reverted_by_id'),
    probation_ends_at = NULL
WHERE id = @id AND workspace_id = @workspace_id AND reverted_at IS NULL
RETURNING *;

-- name: ListDueOrgChangeProbations :many
-- Windows that have ended and have no outcome. The job reads these across all
-- workspaces, in the order they ended.
SELECT * FROM org_change_application
WHERE probation_ends_at IS NOT NULL
  AND probation_ends_at <= @now::timestamptz
  AND probation_outcome IS NULL
  AND reverted_at IS NULL
ORDER BY probation_ends_at ASC, id ASC
LIMIT @row_limit;

-- name: SetOrgChangeProbationOutcome :one
-- Records the outcome once. A window already decided is not written again.
UPDATE org_change_application SET
    probation_outcome = @probation_outcome,
    probation_note = @probation_note,
    probation_checked_at = now(),
    after_scorecard_id = sqlc.narg('after_scorecard_id'),
    rollback_proposal_id = sqlc.narg('rollback_proposal_id')
WHERE id = @id AND probation_outcome IS NULL
RETURNING *;

-- name: UnarchiveSquad :one
UPDATE squad SET archived_at = NULL, archived_by = NULL, updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;

-- name: ListSquadRoutineAutopilotIDs :many
SELECT autopilot_id FROM squad_routine
WHERE workspace_id = @workspace_id AND squad_id = @squad_id
ORDER BY created_at ASC, autopilot_id ASC;

-- name: MoveSquadRoutinesToSquad :exec
UPDATE squad_routine SET squad_id = @to_squad_id
WHERE workspace_id = @workspace_id AND autopilot_id = ANY(@autopilot_ids::uuid[]);

-- name: CancelQueuedTasksByAgent :many
-- Retiring an agent cancels what it has not started. Running work finishes.
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL,
    cancelled_by_type = 'system', cancelled_by_id = NULL, cancelled_by_name = NULL
WHERE agent_id = @agent_id AND status IN ('queued', 'deferred')
RETURNING *;

-- name: CancelQueuedTasksBySquad :many
UPDATE agent_task_queue
SET status = 'cancelled', completed_at = now(), prepare_lease_expires_at = NULL,
    cancelled_by_type = 'system', cancelled_by_id = NULL, cancelled_by_name = NULL
WHERE squad_id = @squad_id AND status IN ('queued', 'deferred')
RETURNING *;

-- name: CountIssuesWithTitle :one
-- Used by the monthly review routine to file its issue once per period.
SELECT COUNT(*)::bigint FROM issue
WHERE workspace_id = @workspace_id AND title = @title;

-- name: ListSquadMemberAgentIDs :many
SELECT member_id FROM squad_member
WHERE squad_id = @squad_id AND member_type = 'agent';

-- name: GetOrgChangeApplicationInWorkspace :one
SELECT * FROM org_change_application
WHERE id = @id AND workspace_id = @workspace_id;

-- name: SetOrgChangeRollbackProposal :exec
UPDATE org_change_application SET rollback_proposal_id = @rollback_proposal_id
WHERE id = @id AND workspace_id = @workspace_id;

-- name: DeleteWorkspaceOrgChangeApplications :exec
-- Workspace teardown: applications have no foreign keys.
DELETE FROM org_change_application WHERE workspace_id = @workspace_id;
