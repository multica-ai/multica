-- name: UpsertKagentEnrollment :one
-- Idempotent enrollment cannot change ownership, reset progress, or resurrect
-- an absent enrollment. No row returned means conflict with existing intent.
INSERT INTO kagent_enrollment (agent_id, workspace_id, runtime_id)
VALUES (sqlc.arg(agent_id), sqlc.arg(workspace_id), sqlc.arg(runtime_id))
ON CONFLICT (agent_id) DO UPDATE SET agent_id = kagent_enrollment.agent_id
WHERE kagent_enrollment.workspace_id = EXCLUDED.workspace_id
  AND kagent_enrollment.runtime_id = EXCLUDED.runtime_id
  AND kagent_enrollment.desired_state = 'present'
RETURNING *;

-- name: GetKagentEnrollment :one
SELECT * FROM kagent_enrollment
WHERE workspace_id = sqlc.arg(workspace_id)
  AND runtime_id = sqlc.arg(runtime_id) AND agent_id = sqlc.arg(agent_id);

-- name: ListKagentEnrollments :many
-- Includes tombstones; callers must not interpret an omitted row as deleted.
SELECT * FROM kagent_enrollment
WHERE workspace_id = sqlc.arg(workspace_id) AND runtime_id = sqlc.arg(runtime_id)
ORDER BY agent_id;

-- name: RequestKagentEnrollmentDesiredState :one
-- Present-to-present requests also advance generation for configuration changes.
-- Absent-to-absent is rejected, preserving deletion progress and tombstones.
UPDATE kagent_enrollment
SET desired_state = sqlc.arg(desired_state), generation = generation + 1,
    revision = revision + 1,
    phase = CASE WHEN sqlc.arg(desired_state)::text = 'present' THEN 'pending' ELSE 'draining' END,
    last_error = '', updated_at = now()
WHERE workspace_id = sqlc.arg(workspace_id) AND runtime_id = sqlc.arg(runtime_id)
  AND agent_id = sqlc.arg(agent_id) AND generation = sqlc.arg(expected_generation)
  AND sqlc.arg(desired_state)::text IN ('present', 'absent')
  AND (desired_state = 'present' OR
      (desired_state = 'absent' AND phase = 'deleted' AND sqlc.arg(desired_state)::text = 'present'))
RETURNING *;

-- name: BeginKagentEnrollmentDelete :one
-- The caller must verify remote quiescence independently. Local task counts
-- cannot establish that remote runs have stopped or that a drain is complete.
UPDATE kagent_enrollment
SET revision = revision + 1, phase = 'deleting', last_error = '', updated_at = now()
WHERE workspace_id = sqlc.arg(workspace_id) AND runtime_id = sqlc.arg(runtime_id)
  AND agent_id = sqlc.arg(agent_id) AND generation = sqlc.arg(expected_generation)
  AND revision = sqlc.arg(expected_revision)
  AND desired_state = 'absent' AND phase = 'draining'
  AND sqlc.arg(remote_quiescence_verified)::boolean
RETURNING *;

-- name: ReportKagentEnrollmentStatus :one
-- Revision CAS rejects delayed reports even after a full phase cycle.
-- PostgreSQL bigint overflow aborts the write rather than reusing a revision.
-- Blocked retries return to pending/draining before advancing. Deletion needs
-- explicit verification of remote removal, independent of local DB task state.
-- last_error must be sanitized by the caller before persistence.
UPDATE kagent_enrollment
SET revision = revision + 1, phase = sqlc.arg(phase), last_error = sqlc.arg(last_error),
    applied_generation = CASE WHEN sqlc.arg(phase)::text IN ('ready', 'deleted')
        THEN generation ELSE applied_generation END,
    updated_at = now()
WHERE workspace_id = sqlc.arg(workspace_id) AND runtime_id = sqlc.arg(runtime_id)
  AND agent_id = sqlc.arg(agent_id) AND generation = sqlc.arg(expected_generation)
  AND revision = sqlc.arg(expected_revision)
  AND phase = sqlc.arg(expected_phase)
  AND (
      (desired_state = 'present' AND (
          (phase = 'pending' AND sqlc.arg(phase)::text IN ('ready', 'blocked')) OR
          (phase = 'ready' AND sqlc.arg(phase)::text = 'blocked') OR
          (phase = 'blocked' AND sqlc.arg(phase)::text = 'pending'))) OR
      (desired_state = 'absent' AND (
          (phase IN ('draining', 'deleting') AND sqlc.arg(phase)::text = 'blocked') OR
          (phase = 'blocked' AND sqlc.arg(phase)::text = 'draining') OR
          (phase = 'deleting' AND sqlc.arg(phase)::text = 'deleted'
              AND sqlc.arg(remote_deletion_verified)::boolean))))
RETURNING *;
