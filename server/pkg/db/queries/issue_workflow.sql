-- Project workflows (MUL-7420). A workflow selects and orders statuses from
-- the workspace's issue_status catalog and can hand an issue to a handler when
-- it enters a step. Projects opt in through project.workflow_id.

-- name: ListIssueWorkflows :many
SELECT * FROM issue_workflow
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
ORDER BY lower(name), created_at, id;

-- name: GetIssueWorkflow :one
SELECT * FROM issue_workflow
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: CreateIssueWorkflow :one
INSERT INTO issue_workflow (workspace_id, name, description, initial_status_key, steps)
VALUES (
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('name')::text,
    sqlc.arg('description')::text,
    sqlc.arg('initial_status_key')::text,
    sqlc.arg('steps')::jsonb
)
RETURNING *;

-- name: UpdateIssueWorkflow :one
UPDATE issue_workflow SET
    name = sqlc.arg('name')::text,
    description = sqlc.arg('description')::text,
    initial_status_key = sqlc.arg('initial_status_key')::text,
    steps = sqlc.arg('steps')::jsonb,
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: DeleteIssueWorkflow :execrows
DELETE FROM issue_workflow
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: DeleteIssueWorkflowsForWorkspace :exec
-- No foreign keys by project rule, so workspace teardown cleans up here.
DELETE FROM issue_workflow WHERE workspace_id = sqlc.arg('workspace_id')::uuid;

-- name: ListIssueWorkflowNamesUsingStatusKey :many
-- Archive guard for the status catalog: a status a workflow still lists cannot
-- be retired, or that workflow's projects would lose a column.
SELECT name FROM issue_workflow
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND steps @> jsonb_build_array(jsonb_build_object('status_key', sqlc.arg('status_key')::text))
ORDER BY lower(name);

-- name: ListProjectsUsingIssueWorkflow :many
SELECT * FROM project
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND workflow_id = sqlc.arg('workflow_id')::uuid
ORDER BY created_at, id;

-- name: CountProjectIssuesByStatus :many
-- Status census for a workflow switch or step removal, including terminal
-- issues: every issue of the project must end on a status its workflow lists.
SELECT status, COUNT(*)::bigint AS issue_count
FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND project_id = ANY(sqlc.arg('project_ids')::uuid[])
GROUP BY status
ORDER BY status;

-- name: RemapProjectIssueStatus :many
-- Moves every issue of the given projects from one status to another as part
-- of an administrative workflow change. Mirrors UpdateIssueStatus: the issue
-- is re-ranked to the top of its new column, the revision advances, and a
-- duplicate mark only survives cancelled -> cancelled (never the case here,
-- because from and to differ). It deliberately touches nothing else, so it
-- never hands an issue off or starts a run.
UPDATE issue AS i SET
    status = sqlc.arg('to_status')::text,
    duplicate_of_issue_id = NULL,
    position = (
        SELECT COALESCE(MIN(target.position), 0) - 1
        FROM issue AS target
        WHERE target.workspace_id = i.workspace_id
          AND target.status = sqlc.arg('to_status')::text
    ),
    revision = i.revision + 1,
    last_activity_at = GREATEST(COALESCE(i.last_activity_at, i.updated_at), now()),
    updated_at = now()
WHERE i.workspace_id = sqlc.arg('workspace_id')::uuid
  AND i.project_id = ANY(sqlc.arg('project_ids')::uuid[])
  AND i.status = sqlc.arg('from_status')::text
  AND i.status <> sqlc.arg('to_status')::text
RETURNING i.*;

-- name: LockProjectForWorkflowChange :one
-- Serializes concurrent workflow switches of one project.
SELECT * FROM project
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
FOR UPDATE;

-- name: SetProjectWorkflow :one
UPDATE project SET
    workflow_id = sqlc.narg('workflow_id')::uuid,
    updated_at = now()
WHERE id = sqlc.arg('id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;
