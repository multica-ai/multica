-- name: ListProjectIssuesForWorkflowMigration :many
-- Caller holds the exclusive workspace catalog lock before any row locks.
SELECT * FROM issue
WHERE workspace_id = $1 AND project_id = $2
ORDER BY id
FOR UPDATE;

-- name: ListProjectMigrationStatuses :many
SELECT s.* FROM issue_workflow_status s
WHERE s.workspace_id = $1 AND (
    s.workflow_id = $3 OR EXISTS (
        SELECT 1 FROM issue i WHERE i.workspace_id = $1 AND i.project_id = $2
        AND i.workflow_id = s.workflow_id
    )
)
ORDER BY s.workflow_id, s.position, s.id;

-- name: CountIssuesUsingWorkflowStatus :one
SELECT count(*) FROM issue WHERE workspace_id = $1 AND workflow_status_id = $2;

-- name: IssueHasActiveWorkflowWork :one
SELECT EXISTS (
    SELECT 1 FROM agent_task_queue t JOIN issue i ON i.id=t.issue_id WHERE i.workspace_id = sqlc.arg(workspace_id) AND t.issue_id = sqlc.arg(issue_id)
      AND t.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
) OR EXISTS (
    SELECT 1 FROM automation_execution e WHERE e.workspace_id = sqlc.arg(workspace_id) AND e.issue_id = sqlc.arg(issue_id)
      AND e.status IN ('pending', 'queued', 'running')
) AS active;

-- name: MigrateIssueWorkflowBinding :one
UPDATE issue i
SET workflow_id = s.workflow_id, workflow_status_id = s.id,
    status = COALESCE(s.legacy_status_key, CASE s.phase
        WHEN 'unstarted' THEN 'todo' WHEN 'done' THEN 'done'
        WHEN 'closed' THEN 'cancelled' ELSE 'in_progress' END),
    revision = i.revision + 1, updated_at = now()
FROM issue_workflow_status s
WHERE i.workspace_id = sqlc.arg(workspace_id) AND i.id = sqlc.arg(issue_id)
  AND s.workspace_id = i.workspace_id AND s.id = sqlc.arg(status_id) AND s.archived_at IS NULL
RETURNING i.*;

-- name: ListWorkflowMigrationViews :many
SELECT * FROM issue_view WHERE workspace_id = $1
AND (scope_type <> 'project' OR scope_id = $2)
AND jsonb_typeof(query->'statusFilters') = 'array'
AND query->'statusFilters' <> '[]'::jsonb
ORDER BY id FOR UPDATE;

-- name: MigrateIssueViewQuery :exec
UPDATE issue_view SET query = $3, revision = revision + 1, updated_at = now()
WHERE workspace_id = $1 AND id = $2;
