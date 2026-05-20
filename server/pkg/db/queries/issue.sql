-- name: ListIssues :many
-- involves_user_id widens the assignee filter to surface issues where the user
-- is *indirectly* the assignee — via an owned agent or a squad they belong to /
-- lead / have an agent inside. The semantics intentionally exclude direct
-- member assignment (`assignee_type='member' AND assignee_id=involves_user_id`)
-- because that is already the meaning of the `assignee_id` filter (tab 1
-- "Assigned to me"), and the two filters must produce disjoint result sets.
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id
FROM issue i
WHERE i.workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (COALESCE(cardinality(sqlc.narg('priorities')::text[]), 0) = 0 OR i.priority = ANY(sqlc.narg('priorities')::text[]))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (
    (
      COALESCE(cardinality(sqlc.narg('assignee_pairs')::text[]), 0) = 0
      AND sqlc.narg('include_no_assignee')::boolean IS NOT TRUE
    )
    OR (sqlc.narg('include_no_assignee')::boolean IS TRUE AND i.assignee_id IS NULL)
    OR (i.assignee_id IS NOT NULL AND (i.assignee_type || ':' || i.assignee_id::text) = ANY(sqlc.narg('assignee_pairs')::text[]))
  )
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (
    COALESCE(cardinality(sqlc.narg('creator_pairs')::text[]), 0) = 0
    OR (i.creator_type || ':' || i.creator_id::text) = ANY(sqlc.narg('creator_pairs')::text[])
  )
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (
    (
      COALESCE(cardinality(sqlc.narg('project_ids')::uuid[]), 0) = 0
      AND sqlc.narg('include_no_project')::boolean IS NOT TRUE
    )
    OR (sqlc.narg('include_no_project')::boolean IS TRUE AND i.project_id IS NULL)
    OR (i.project_id = ANY(sqlc.narg('project_ids')::uuid[]))
  )
  AND (sqlc.narg('scheduled')::bool IS NULL OR (i.start_date IS NOT NULL OR i.due_date IS NOT NULL))
  AND (
    sqlc.narg('involves_user_id')::uuid IS NULL
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = $1
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
            JOIN agent a ON a.id = sm.member_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'agent'
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
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
    parent_issue_id, position, start_date, due_date, number, project_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
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
    parent_issue_id, position, start_date, due_date, number, project_id,
    origin_type, origin_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    sqlc.narg('origin_type'), sqlc.narg('origin_id')
) RETURNING *;

-- name: LockIssueDuplicateKey :exec
SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0));

-- name: FindActiveDuplicateIssue :one
SELECT * FROM issue
WHERE workspace_id = $1
  AND status NOT IN ('done', 'cancelled')
  AND project_id IS NOT DISTINCT FROM sqlc.arg('project_id')::uuid
  AND parent_issue_id IS NOT DISTINCT FROM sqlc.arg('parent_issue_id')::uuid
  AND lower(btrim(regexp_replace(title, '[[:space:]]+', ' ', 'g'))) = sqlc.arg('normalized_title')
ORDER BY created_at ASC
LIMIT 1;

-- name: DeleteIssue :exec
DELETE FROM issue WHERE id = $1;

-- name: ListOpenIssues :many
-- See ListIssues for the semantics of involves_user_id (mirrors the 4-branch
-- filter; member-direct assignment is intentionally excluded).
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id
FROM issue i
WHERE i.workspace_id = $1
  AND i.status NOT IN ('done', 'cancelled')
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (COALESCE(cardinality(sqlc.narg('priorities')::text[]), 0) = 0 OR i.priority = ANY(sqlc.narg('priorities')::text[]))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (
    (
      COALESCE(cardinality(sqlc.narg('assignee_pairs')::text[]), 0) = 0
      AND sqlc.narg('include_no_assignee')::boolean IS NOT TRUE
    )
    OR (sqlc.narg('include_no_assignee')::boolean IS TRUE AND i.assignee_id IS NULL)
    OR (i.assignee_id IS NOT NULL AND (i.assignee_type || ':' || i.assignee_id::text) = ANY(sqlc.narg('assignee_pairs')::text[]))
  )
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (
    COALESCE(cardinality(sqlc.narg('creator_pairs')::text[]), 0) = 0
    OR (i.creator_type || ':' || i.creator_id::text) = ANY(sqlc.narg('creator_pairs')::text[])
  )
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (
    (
      COALESCE(cardinality(sqlc.narg('project_ids')::uuid[]), 0) = 0
      AND sqlc.narg('include_no_project')::boolean IS NOT TRUE
    )
    OR (sqlc.narg('include_no_project')::boolean IS TRUE AND i.project_id IS NULL)
    OR (i.project_id = ANY(sqlc.narg('project_ids')::uuid[]))
  )
  AND (
    sqlc.narg('involves_user_id')::uuid IS NULL
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = $1
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
            JOIN agent a ON a.id = sm.member_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'agent'
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
  )
ORDER BY i.position ASC, i.created_at DESC;

-- name: CountIssues :one
-- See ListIssues for the semantics of involves_user_id.
SELECT count(*) FROM issue i
WHERE i.workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (COALESCE(cardinality(sqlc.narg('priorities')::text[]), 0) = 0 OR i.priority = ANY(sqlc.narg('priorities')::text[]))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (
    (
      COALESCE(cardinality(sqlc.narg('assignee_pairs')::text[]), 0) = 0
      AND sqlc.narg('include_no_assignee')::boolean IS NOT TRUE
    )
    OR (sqlc.narg('include_no_assignee')::boolean IS TRUE AND i.assignee_id IS NULL)
    OR (i.assignee_id IS NOT NULL AND (i.assignee_type || ':' || i.assignee_id::text) = ANY(sqlc.narg('assignee_pairs')::text[]))
  )
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (
    COALESCE(cardinality(sqlc.narg('creator_pairs')::text[]), 0) = 0
    OR (i.creator_type || ':' || i.creator_id::text) = ANY(sqlc.narg('creator_pairs')::text[])
  )
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (
    (
      COALESCE(cardinality(sqlc.narg('project_ids')::uuid[]), 0) = 0
      AND sqlc.narg('include_no_project')::boolean IS NOT TRUE
    )
    OR (sqlc.narg('include_no_project')::boolean IS TRUE AND i.project_id IS NULL)
    OR (i.project_id = ANY(sqlc.narg('project_ids')::uuid[]))
  )
  AND (sqlc.narg('scheduled')::bool IS NULL OR (i.start_date IS NOT NULL OR i.due_date IS NOT NULL))
  AND (
    sqlc.narg('involves_user_id')::uuid IS NULL
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = $1
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
          UNION
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
            JOIN agent a ON a.id = sm.member_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'agent'
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
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
