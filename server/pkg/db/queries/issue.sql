-- CEREBRO-PATCH(sqlc-issue): cerebro modification of upstream file
-- CEREBRO-PATCH(nested-projects): project filters can include descendant project IDs.
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
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at,
       i.number, i.project_id, i.kind, i.is_private
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
        -- CEREBRO-PATCH(list-issues-group-access): JEH-1009 cascade project-group access into issue visibility.
        OR EXISTS (SELECT 1 FROM cerebro_project_group_member pgm JOIN cerebro_group_member gm ON gm.group_id = pgm.group_id WHERE pgm.project_id = p.id AND gm.user_id = sqlc.arg('user_id')::uuid)
      )
    )
  )
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('scheduled')::bool IS NULL OR (i.start_date IS NOT NULL OR i.due_date IS NOT NULL))
  AND (
    sqlc.narg('project_ids')::uuid[] IS NULL
    OR i.project_id = ANY(sqlc.narg('project_ids')::uuid[])
  )
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
    parent_issue_id, position, start_date, due_date, number, project_id, kind
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
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
    start_date = sqlc.narg('start_date'),
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
-- CEREBRO-PATCH(create-issue-origin-private): autopilot-created issues can inherit privacy from the source autopilot (JEH-1749).
INSERT INTO issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    origin_type, origin_id, kind, is_private
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    sqlc.narg('origin_type'), sqlc.narg('origin_id'),
    COALESCE(sqlc.narg('kind')::text, 'issue'),
    COALESCE(sqlc.narg('is_private')::boolean, FALSE)
) RETURNING *;

-- name: LockIssueDuplicateKey :exec
SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0));

-- name: FindActiveDuplicateIssue :one
SELECT * FROM issue
WHERE workspace_id = $1
  AND status NOT IN ('done', 'cancelled')
  AND project_id IS NOT DISTINCT FROM sqlc.arg('project_id')::uuid
  AND parent_issue_id IS NOT DISTINCT FROM sqlc.arg('parent_issue_id')::uuid
  AND lower(btrim(regexp_replace(title, '[[:space:]]+', ' ', 'g'))) = sqlc.arg('normalized_title')::text
ORDER BY created_at ASC
LIMIT 1;

-- name: DeleteIssue :exec
DELETE FROM issue WHERE id = $1;

-- name: ListOpenIssues :many
SELECT id, workspace_id, title, description, status, priority,
       assignee_type, assignee_id, creator_type, creator_id,
       parent_issue_id, position, start_date, due_date, created_at, updated_at, number, project_id, kind
FROM issue
WHERE workspace_id = $1
  AND kind = 'issue'
  AND status NOT IN ('done', 'cancelled')
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR creator_id = sqlc.narg('creator_id'))
  AND (
    sqlc.narg('project_ids')::uuid[] IS NULL
    OR project_id = ANY(sqlc.narg('project_ids')::uuid[])
  )
ORDER BY position ASC, created_at DESC;

-- name: CountIssues :one
-- CEREBRO-PATCH(count-issues-access-filter): keep list totals aligned with private/project visibility (JEH-1749).
SELECT count(*) FROM issue
LEFT JOIN project p ON p.id = issue.project_id
WHERE issue.workspace_id = $1
  AND issue.kind = 'issue'
  AND (
    sqlc.arg('is_admin')::boolean
    OR (
      issue.project_id IS NULL AND (
        issue.is_private = FALSE
        OR (issue.creator_type = 'member' AND issue.creator_id = sqlc.arg('user_id')::uuid)
      )
    )
    OR (
      issue.project_id IS NOT NULL AND (
        p.access = 'workspace'
        OR EXISTS (
          SELECT 1 FROM project_member pm
          WHERE pm.project_id = p.id AND pm.user_id = sqlc.arg('user_id')::uuid
        )
        OR EXISTS (SELECT 1 FROM cerebro_project_group_member pgm JOIN cerebro_group_member gm ON gm.group_id = pgm.group_id WHERE pgm.project_id = p.id AND gm.user_id = sqlc.arg('user_id')::uuid)
      )
    )
  )
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('scheduled')::bool IS NULL OR (start_date IS NOT NULL OR due_date IS NOT NULL))
  AND (
    sqlc.narg('project_ids')::uuid[] IS NULL
    OR project_id = ANY(sqlc.narg('project_ids')::uuid[])
  );

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

-- CEREBRO-PATCH(workflow-reassign-issue): JEH-1108 reassign_issue action needs an assignee-only UPDATE.
-- name: UpdateIssueAssignee :one
-- Narrow assignee-only mutation used by the cerebro workflow engine's
-- reassign_issue action. Distinct from UpdateIssue (which clears nullable
-- columns when args are nil) so the action cannot accidentally wipe an
-- unrelated optional field while reassigning.
UPDATE issue SET
    assignee_type = $2,
    assignee_id = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;
