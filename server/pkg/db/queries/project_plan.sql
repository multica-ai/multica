-- name: LockWorkspaceForProjectPlanWrite :one
SELECT id FROM workspace
WHERE id = $1
FOR KEY SHARE;

-- name: LockProjectForProjectPlanWrite :one
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: GetProjectPlanKindForWrite :one
SELECT * FROM project_plan_kind
WHERE key = $1
FOR KEY SHARE;

-- name: GetProjectPlanSourceIssueForWrite :one
SELECT id, workspace_id, project_id, title, description, number, revision
FROM issue
WHERE id = $1 AND workspace_id = $2 AND project_id = $3
FOR KEY SHARE;

-- name: GetProjectPlanForWrite :one
SELECT * FROM project_plan
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: GetProjectPlan :one
SELECT * FROM project_plan
WHERE id = $1 AND workspace_id = $2;

-- name: GetActiveProjectPlanForRead :one
SELECT id, workspace_id, project_id, version, kind, origin, title, description,
       attributes, source_issue_id, source_issue_revision,
       source_description_snapshot, source_content_sha256,
       created_by_type, created_by_id, superseded_at, created_at, updated_at
FROM project_plan
WHERE project_id = $1
  AND workspace_id = $2
  AND superseded_at IS NULL;

-- name: GetProjectPlanForRead :one
SELECT id, workspace_id, project_id, version, kind, origin, title, description,
       attributes, source_issue_id, source_issue_revision,
       source_description_snapshot, source_content_sha256,
       created_by_type, created_by_id, superseded_at, created_at, updated_at
FROM project_plan
WHERE id = $1
  AND project_id = $2
  AND workspace_id = $3;

-- name: ListProjectPlanRollups :many
WITH part_rollup AS (
    SELECT
        part.id,
        part.project_plan_id,
        part.project_plan_phase_id,
        part.title,
        part.description,
        part.acceptance_criteria,
        part.attributes,
        part.position,
        part.created_at,
        part.updated_at,
        COUNT(link.id)::bigint AS membership_rows,
        COUNT(issue.id) FILTER (
            WHERE issue_effective_status(issue.workspace_id, issue.status) <> 'cancelled'
        )::bigint AS tasks_total,
        COUNT(issue.id) FILTER (
            WHERE issue_effective_status(issue.workspace_id, issue.status) = 'done'
        )::bigint AS tasks_done,
        COUNT(issue.id) FILTER (
            WHERE issue_effective_status(issue.workspace_id, issue.status)
                NOT IN ('backlog', 'todo', 'cancelled')
        )::bigint AS tasks_started
    FROM project_plan_part AS part
    LEFT JOIN project_plan_part_issue AS link
      ON link.project_plan_id = part.project_plan_id
     AND link.project_plan_part_id = part.id
    LEFT JOIN issue
      ON issue.id = link.issue_id
     AND issue.workspace_id = sqlc.arg('workspace_id')
     AND issue.project_id = sqlc.arg('project_id')
    WHERE part.project_plan_id = sqlc.arg('project_plan_id')
    GROUP BY
        part.id,
        part.project_plan_id,
        part.project_plan_phase_id,
        part.title,
        part.description,
        part.acceptance_criteria,
        part.attributes,
        part.position,
        part.created_at,
        part.updated_at
)
SELECT
    phase.id AS phase_id,
    phase.title AS phase_title,
    phase.description AS phase_description,
    phase.attributes AS phase_attributes,
    phase.position AS phase_position,
    phase.created_at AS phase_created_at,
    phase.updated_at AS phase_updated_at,
    part.id AS part_id,
    part.title AS part_title,
    part.description AS part_description,
    part.acceptance_criteria AS part_acceptance_criteria,
    part.attributes AS part_attributes,
    part.position AS part_position,
    part.created_at AS part_created_at,
    part.updated_at AS part_updated_at,
    COALESCE(part.membership_rows, 0)::bigint AS part_membership_rows,
    COALESCE(part.tasks_total, 0)::bigint AS part_tasks_total,
    COALESCE(part.tasks_done, 0)::bigint AS part_tasks_done,
    COALESCE(part.tasks_started, 0)::bigint AS part_tasks_started,
    COALESCE(SUM(part.tasks_total) OVER (PARTITION BY phase.id), 0)::bigint AS phase_tasks_total,
    COALESCE(SUM(part.tasks_done) OVER (PARTITION BY phase.id), 0)::bigint AS phase_tasks_done,
    COALESCE(SUM(part.tasks_total) OVER (), 0)::bigint AS plan_tasks_total,
    COALESCE(SUM(part.tasks_done) OVER (), 0)::bigint AS plan_tasks_done,
    COUNT(part.id) OVER ()::bigint AS plan_parts_total,
    COUNT(part.id) FILTER (WHERE part.membership_rows > 0) OVER ()::bigint AS plan_parts_covered,
    COUNT(part.id) FILTER (WHERE part.membership_rows = 0) OVER ()::bigint AS plan_parts_without_tasks
FROM project_plan_phase AS phase
LEFT JOIN part_rollup AS part
  ON part.project_plan_id = phase.project_plan_id
 AND part.project_plan_phase_id = phase.id
WHERE phase.project_plan_id = sqlc.arg('project_plan_id')
ORDER BY phase.position, part.position NULLS LAST, part.id;

-- name: ListProjectPlanIssueDetails :many
SELECT
    link.project_plan_part_id,
    issue.id AS issue_id,
    COALESCE(issue.number, link.issue_number_snapshot)::integer AS issue_number,
    COALESCE(issue.title, link.issue_title_snapshot)::text AS issue_title,
    issue.status AS issue_status,
    CASE
        WHEN issue.id IS NULL THEN 'deleted'
        ELSE issue_effective_status(issue.workspace_id, issue.status)
    END::text AS issue_status_category,
    issue.assignee_type,
    issue.assignee_id,
    (issue.id IS NULL)::boolean AS issue_deleted,
    workspace.issue_prefix
FROM project_plan_part_issue AS link
JOIN workspace ON workspace.id = sqlc.arg('workspace_id')
LEFT JOIN issue
  ON issue.id = link.issue_id
 AND issue.workspace_id = sqlc.arg('workspace_id')
 AND issue.project_id = sqlc.arg('project_id')
WHERE link.project_plan_id = sqlc.arg('project_plan_id')
ORDER BY link.project_plan_part_id, link.created_at, link.id;

-- name: ListProjectPlanDependenciesForRead :many
SELECT id, project_plan_id, blocked_phase_id, blocked_part_id,
       blocking_phase_id, blocking_part_id, created_at, updated_at
FROM project_plan_dependency
WHERE project_plan_id = $1
ORDER BY created_at, id;

-- name: ListProjectPlanUncoveredParts :many
SELECT
    part.id AS part_id,
    part.title AS part_title,
    phase.id AS phase_id,
    phase.title AS phase_title,
    phase.position AS phase_position,
    part.position AS part_position
FROM project_plan_part AS part
JOIN project_plan_phase AS phase
  ON phase.project_plan_id = part.project_plan_id
 AND phase.id = part.project_plan_phase_id
LEFT JOIN project_plan_part_issue AS link
  ON link.project_plan_id = part.project_plan_id
 AND link.project_plan_part_id = part.id
WHERE part.project_plan_id = $1
  AND link.id IS NULL
ORDER BY phase.position, part.position, part.id;

-- name: GetActiveProjectPlanForWrite :one
SELECT * FROM project_plan
WHERE project_id = $1 AND workspace_id = $2 AND superseded_at IS NULL
FOR UPDATE;

-- name: GetNextProjectPlanVersion :one
SELECT (COALESCE(MAX(version), 0) + 1)::integer
FROM project_plan
WHERE project_id = $1;

-- name: CreateProjectPlan :one
INSERT INTO project_plan (
    workspace_id,
    project_id,
    version,
    kind,
    origin,
    title,
    description,
    attributes,
    source_issue_id,
    source_issue_revision,
    source_description_snapshot,
    source_content_sha256,
    created_by_type,
    created_by_id
) VALUES (
    sqlc.arg('workspace_id'),
    sqlc.arg('project_id'),
    sqlc.arg('version'),
    sqlc.arg('kind'),
    sqlc.arg('origin'),
    sqlc.arg('title'),
    sqlc.arg('description'),
    sqlc.narg('attributes'),
    sqlc.narg('source_issue_id'),
    sqlc.narg('source_issue_revision'),
    sqlc.narg('source_description_snapshot'),
    sqlc.narg('source_content_sha256'),
    sqlc.arg('created_by_type'),
    sqlc.arg('created_by_id')
)
RETURNING *;

-- name: SupersedeProjectPlan :one
UPDATE project_plan
SET superseded_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND superseded_at IS NULL
RETURNING *;

-- name: UpdateProjectPlanTitle :one
UPDATE project_plan SET title = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND superseded_at IS NULL
RETURNING *;

-- name: UpdateProjectPlanDescription :one
UPDATE project_plan SET description = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND superseded_at IS NULL
RETURNING *;

-- name: UpdateProjectPlanKind :one
UPDATE project_plan SET kind = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND superseded_at IS NULL
RETURNING *;

-- name: UpdateProjectPlanAttributes :one
UPDATE project_plan SET attributes = sqlc.narg('attributes'), updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id') AND superseded_at IS NULL
RETURNING *;

-- name: ListProjectPlanPhasesForClone :many
SELECT * FROM project_plan_phase
WHERE project_plan_id = $1
ORDER BY position, id;

-- name: CreateProjectPlanPhase :one
INSERT INTO project_plan_phase (
    project_plan_id, title, description, attributes, position
) VALUES (
    sqlc.arg('project_plan_id'), sqlc.arg('title'), sqlc.arg('description'),
    sqlc.narg('attributes'), sqlc.arg('position')
)
RETURNING *;

-- name: GetProjectPlanPhaseForWrite :one
SELECT phase.*
FROM project_plan_phase AS phase
JOIN project_plan AS plan ON plan.id = phase.project_plan_id
WHERE phase.id = $1 AND phase.project_plan_id = $2 AND plan.workspace_id = $3
FOR UPDATE OF phase;

-- name: ListProjectPlanPhaseIDs :many
SELECT id FROM project_plan_phase
WHERE project_plan_id = $1
ORDER BY position, id;

-- name: UpdateProjectPlanPhaseTitle :one
UPDATE project_plan_phase SET title = $3, updated_at = now()
WHERE id = $1 AND project_plan_id = $2
RETURNING *;

-- name: UpdateProjectPlanPhaseDescription :one
UPDATE project_plan_phase SET description = $3, updated_at = now()
WHERE id = $1 AND project_plan_id = $2
RETURNING *;

-- name: UpdateProjectPlanPhaseAttributes :one
UPDATE project_plan_phase SET attributes = sqlc.narg('attributes'), updated_at = now()
WHERE id = sqlc.arg('id') AND project_plan_id = sqlc.arg('project_plan_id')
RETURNING *;

-- name: UpdateProjectPlanPhasePosition :one
UPDATE project_plan_phase SET position = $3, updated_at = now()
WHERE id = $1 AND project_plan_id = $2
RETURNING *;

-- name: ListProjectPlanPartsForClone :many
SELECT * FROM project_plan_part
WHERE project_plan_id = $1
ORDER BY project_plan_phase_id, position, id;

-- name: CreateProjectPlanPart :one
INSERT INTO project_plan_part (
    project_plan_id, project_plan_phase_id, title, description,
    acceptance_criteria, attributes, position
) VALUES (
    sqlc.arg('project_plan_id'), sqlc.arg('project_plan_phase_id'),
    sqlc.arg('title'), sqlc.arg('description'), sqlc.arg('acceptance_criteria'),
    sqlc.narg('attributes'), sqlc.arg('position')
)
RETURNING *;

-- name: GetProjectPlanPartForWrite :one
SELECT part.*
FROM project_plan_part AS part
JOIN project_plan AS plan ON plan.id = part.project_plan_id
WHERE part.id = $1 AND part.project_plan_id = $2 AND plan.workspace_id = $3
FOR UPDATE OF part;

-- name: ListProjectPlanPartIDs :many
SELECT id FROM project_plan_part
WHERE project_plan_id = $1 AND project_plan_phase_id = $2
ORDER BY position, id;

-- name: UpdateProjectPlanPartTitle :one
UPDATE project_plan_part SET title = $3, updated_at = now()
WHERE id = $1 AND project_plan_id = $2
RETURNING *;

-- name: UpdateProjectPlanPartDescription :one
UPDATE project_plan_part SET description = $3, updated_at = now()
WHERE id = $1 AND project_plan_id = $2
RETURNING *;

-- name: UpdateProjectPlanPartAcceptanceCriteria :one
UPDATE project_plan_part SET acceptance_criteria = $3, updated_at = now()
WHERE id = $1 AND project_plan_id = $2
RETURNING *;

-- name: UpdateProjectPlanPartAttributes :one
UPDATE project_plan_part SET attributes = sqlc.narg('attributes'), updated_at = now()
WHERE id = sqlc.arg('id') AND project_plan_id = sqlc.arg('project_plan_id')
RETURNING *;

-- name: UpdateProjectPlanPartPosition :one
UPDATE project_plan_part SET position = $3, updated_at = now()
WHERE id = $1 AND project_plan_id = $2
RETURNING *;

-- name: ListProjectPlanPartIssuesForClone :many
SELECT * FROM project_plan_part_issue
WHERE project_plan_id = $1
ORDER BY id;

-- name: CreateProjectPlanPartIssue :one
INSERT INTO project_plan_part_issue (
    project_plan_id, project_plan_part_id, issue_id,
    issue_number_snapshot, issue_title_snapshot
) VALUES (
    sqlc.arg('project_plan_id'), sqlc.arg('project_plan_part_id'),
    sqlc.narg('issue_id'), sqlc.arg('issue_number_snapshot'),
    sqlc.arg('issue_title_snapshot')
)
RETURNING *;

-- name: DeleteProjectPlanPartIssue :one
DELETE FROM project_plan_part_issue
WHERE project_plan_id = $1 AND project_plan_part_id = $2 AND issue_id = $3
RETURNING *;

-- name: CountProjectPlanDeleteImpact :one
SELECT COUNT(*)::bigint AS membership_rows,
       COUNT(issue_id)::bigint AS live_issues_unlinked
FROM project_plan_part_issue
WHERE project_plan_id = $1;

-- name: ListProjectPlanDependenciesForClone :many
SELECT * FROM project_plan_dependency
WHERE project_plan_id = $1
ORDER BY id;

-- name: CreateProjectPlanDependency :one
INSERT INTO project_plan_dependency (
    project_plan_id, blocked_phase_id, blocked_part_id,
    blocking_phase_id, blocking_part_id
) VALUES (
    sqlc.arg('project_plan_id'), sqlc.narg('blocked_phase_id'),
    sqlc.narg('blocked_part_id'), sqlc.narg('blocking_phase_id'),
    sqlc.narg('blocking_part_id')
)
RETURNING *;

-- name: DeleteProjectPlanDependencies :exec
DELETE FROM project_plan_dependency WHERE project_plan_id = $1;

-- name: DeleteProjectPlanPhaseDependencies :exec
DELETE FROM project_plan_dependency AS dependency
WHERE dependency.project_plan_id = sqlc.arg('project_plan_id')
  AND (
      dependency.blocked_phase_id = sqlc.arg('project_plan_phase_id')
      OR dependency.blocking_phase_id = sqlc.arg('project_plan_phase_id')
      OR dependency.blocked_part_id IN (
          SELECT part.id FROM project_plan_part AS part
          WHERE part.project_plan_id = sqlc.arg('project_plan_id')
            AND part.project_plan_phase_id = sqlc.arg('project_plan_phase_id')
      )
      OR dependency.blocking_part_id IN (
          SELECT part.id FROM project_plan_part AS part
          WHERE part.project_plan_id = sqlc.arg('project_plan_id')
            AND part.project_plan_phase_id = sqlc.arg('project_plan_phase_id')
      )
  );

-- name: DeleteProjectPlanPartDependencies :exec
DELETE FROM project_plan_dependency
WHERE project_plan_id = $1
  AND (blocked_part_id = $2 OR blocking_part_id = $2);

-- name: DeleteProjectPlanPartIssues :exec
DELETE FROM project_plan_part_issue WHERE project_plan_id = $1;

-- name: DeleteProjectPlanPhasePartIssues :exec
DELETE FROM project_plan_part_issue AS part_issue
WHERE part_issue.project_plan_id = sqlc.arg('project_plan_id')
  AND part_issue.project_plan_part_id IN (
      SELECT part.id FROM project_plan_part AS part
      WHERE part.project_plan_id = sqlc.arg('project_plan_id')
        AND part.project_plan_phase_id = sqlc.arg('project_plan_phase_id')
  );

-- name: DeleteProjectPlanPartPartIssues :exec
DELETE FROM project_plan_part_issue
WHERE project_plan_id = $1 AND project_plan_part_id = $2;

-- name: DeleteProjectPlanParts :exec
DELETE FROM project_plan_part WHERE project_plan_id = $1;

-- name: DeleteProjectPlanPhaseParts :exec
DELETE FROM project_plan_part
WHERE project_plan_id = $1 AND project_plan_phase_id = $2;

-- name: DeleteProjectPlanPart :exec
DELETE FROM project_plan_part
WHERE id = $1 AND project_plan_id = $2;

-- name: DeleteProjectPlanPhases :exec
DELETE FROM project_plan_phase WHERE project_plan_id = $1;

-- name: DeleteProjectPlanPhase :exec
DELETE FROM project_plan_phase
WHERE id = $1 AND project_plan_id = $2;

-- name: DeleteProjectPlan :exec
DELETE FROM project_plan
WHERE id = $1 AND workspace_id = $2;
