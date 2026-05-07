-- CEREBRO-PATCH(sqlc-issue): cerebro modification of upstream file
-- name: ListIssues :many
-- Access-filtered list of issues. The access predicate enforces:
--   * is_admin = TRUE  → no filter (workspace owners/admins always see all)
--   * standalone issue + is_private = FALSE → visible
--   * standalone issue + is_private = TRUE  → only creator
--   * project issue:
--       project.access = 'workspace' → visible
--       project.access = 'restricted' AND user is project_member → visible
SELECT i.id, i.workspace_id, i.title, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.due_date, i.created_at, i.updated_at,
       i.number, i.project_id, i.kind
FROM issue i
LEFT JOIN project p ON p.id = i.project_id
WHERE i.workspace_id = $1
  AND i.kind = 'issue'
  AND (
    sqlc.arg('is_admin')::boolean
    OR (
      i.project_id IS NULL AND (
        i.is_private = FALSE
        OR (i.creator_type = 'member' AND i.creator_id = sqlc.arg('user_id')::uuid)
      )
    )
    OR (
      i.project_id IS NOT NULL AND (
        p.access = 'workspace'
        OR EXISTS (
          SELECT 1 FROM project_member pm
          WHERE pm.project_id = p.id AND pm.user_id = sqlc.arg('user_id')::uuid
        )
      )
    )
  )
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
ORDER BY i.position ASC, i.created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetIssue :one
SELECT * FROM issue
WHERE id = $1;

-- name: GetIssueInWorkspace :one
SELECT * FROM issue
WHERE id = $1 AND workspace_id = $2;

-- name: CreateIssue :one
INSERT INTO issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, due_date, number, project_id, kind
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
    COALESCE(sqlc.narg('kind')::text, 'issue')
) RETURNING *;

-- name: GetIssueByNumber :one
SELECT * FROM issue
WHERE workspace_id = $1 AND number = $2;

-- name: UpdateIssue :one
UPDATE issue SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    assignee_type = sqlc.narg('assignee_type'),
    assignee_id = sqlc.narg('assignee_id'),
    position = COALESCE(sqlc.narg('position'), position),
    due_date = sqlc.narg('due_date'),
    parent_issue_id = sqlc.narg('parent_issue_id'),
    project_id = sqlc.narg('project_id'),
    is_private = COALESCE(sqlc.narg('is_private'), is_private),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateIssueStatus :one
UPDATE issue SET
    status = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateIssueWithOrigin :one
INSERT INTO issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, due_date, number, project_id,
    origin_type, origin_id, kind
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
    sqlc.narg('origin_type'), sqlc.narg('origin_id'),
    COALESCE(sqlc.narg('kind')::text, 'issue')
) RETURNING *;

-- name: DeleteIssue :exec
DELETE FROM issue WHERE id = $1;

-- name: ListOpenIssues :many
SELECT id, workspace_id, title, description, status, priority,
       assignee_type, assignee_id, creator_type, creator_id,
       parent_issue_id, position, due_date, created_at, updated_at, number, project_id, kind
FROM issue
WHERE workspace_id = $1
  AND kind = 'issue'
  AND status NOT IN ('done', 'cancelled')
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
ORDER BY position ASC, created_at DESC;

-- name: CountIssues :one
SELECT count(*) FROM issue
WHERE workspace_id = $1
  AND kind = 'issue'
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'));

-- name: ListChildIssues :many
SELECT * FROM issue
WHERE parent_issue_id = $1
ORDER BY position ASC, created_at DESC;

-- name: GetIssueByOrigin :one
-- Finds the issue stamped with a specific (origin_type, origin_id) pair.
-- Used by quick-create completion to deterministically locate the issue
-- produced by a given agent_task_queue.id — robust against concurrent
-- issue creates by the same agent (assignment task + quick-create both
-- running with max_concurrent_tasks > 1).
SELECT * FROM issue
WHERE workspace_id = $1
  AND origin_type = $2
  AND origin_id = $3
LIMIT 1;

-- name: CountCreatedIssueAssignees :many
-- Count assignees on issues created by a specific user.
SELECT
  assignee_type,
  assignee_id,
  COUNT(*)::bigint as frequency
FROM issue
WHERE workspace_id = $1
  AND creator_id = $2
  AND creator_type = 'member'
  AND assignee_type IS NOT NULL
  AND assignee_id IS NOT NULL
GROUP BY assignee_type, assignee_id;

-- name: ChildIssueProgress :many
SELECT parent_issue_id,
       COUNT(*)::bigint AS total,
       COUNT(*) FILTER (WHERE status IN ('done', 'cancelled'))::bigint AS done
FROM issue
WHERE workspace_id = $1
  AND parent_issue_id IS NOT NULL
GROUP BY parent_issue_id;

-- SearchIssues: moved to handler (dynamic SQL for multi-word search support).

-- name: MarkIssueFirstExecuted :one
-- Flips first_executed_at from NULL to now() atomically. Returns the row if
-- this was the first time the issue was executed; no rows otherwise. The
-- analytics issue_executed event fires exactly when this returns a row —
-- retries and re-assignments hit the WHERE clause and no-op.
UPDATE issue
SET first_executed_at = now()
WHERE id = $1 AND first_executed_at IS NULL
RETURNING id, workspace_id, creator_type, creator_id, first_executed_at;
