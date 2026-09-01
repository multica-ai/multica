-- name: ListIssues :many
-- involves_user_id widens the assignee filter to surface issues where the user
-- is *indirectly* the assignee — via an owned agent or a squad they belong to /
-- lead / have an agent inside. The semantics intentionally exclude direct
-- member assignment (`assignee_type='member' AND assignee_id=involves_user_id`)
-- because that is already the meaning of the `assignee_id` filter (tab 1
-- "Assigned to me"), and the two filters must produce disjoint result sets.
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.last_activity_at, i.number, i.project_id, i.metadata, i.stage, i.properties,
       i.revision
FROM issue i
WHERE i.workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR i.status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('scheduled')::bool IS NULL OR (i.start_date IS NOT NULL OR i.due_date IS NOT NULL))
  AND (sqlc.narg('metadata_filter')::jsonb IS NULL OR i.metadata @> sqlc.narg('metadata_filter')::jsonb)
  AND (
    sqlc.narg('involves_user_id')::uuid IS NULL
    -- (1) assignee is an agent owned by the user
    OR (i.assignee_type = 'agent' AND i.assignee_id IN (
          SELECT a.id FROM agent a
           WHERE a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
    ))
    -- (2)(3)(4) assignee is a squad related to the user — three relations
    OR (i.assignee_type = 'squad' AND i.assignee_id IN (
          -- (2) the user is a human member of the squad
          SELECT sm.squad_id
            FROM squad_member sm
            JOIN squad s ON s.id = sm.squad_id
           WHERE s.workspace_id = $1
             AND sm.member_type = 'member'
             AND sm.member_id   = sqlc.narg('involves_user_id')::uuid
          UNION
          -- (3) the squad's canonical leader is an agent owned by the user.
          -- We read squad.leader_id directly rather than relying on a
          -- squad_member row, because the leader copy in squad_member is
          -- best-effort (see squad.go AddSquadMember error handling).
          SELECT s.id
            FROM squad s
            JOIN agent a ON a.id = s.leader_id
           WHERE s.workspace_id = $1
             AND a.workspace_id = $1
             AND a.owner_id     = sqlc.narg('involves_user_id')::uuid
          UNION
          -- (4) the squad has an agent member owned by the user
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

-- name: GetIssueGCStatus :one
SELECT workspace_id, status, updated_at
FROM issue
WHERE id = $1;

-- name: ListIssueGCStatuses :many
SELECT id, status, updated_at
FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND id = ANY(sqlc.arg('issue_ids')::uuid[]);

-- name: GetIssueInWorkspace :one
SELECT * FROM issue
WHERE id = $1 AND workspace_id = $2;

-- name: LockIssueForChannelMediaBind :one
-- Channel media resolves after /issue creation. Hold a key-share lock while
-- the attachment row is written so a concurrent issue delete cannot land
-- between the workspace-scoped validation and the attachment insert.
SELECT id FROM issue
WHERE id = $1 AND workspace_id = $2
FOR KEY SHARE;

-- name: LockIssueForDescriptionUpdate :one
-- Serialize field-baseline checks and combined attachment binding on the
-- owner row. The handler merges channel media that landed after the editor's
-- submitted base, updates the issue, and binds this request's attachments in
-- the same transaction while holding this lock.
SELECT * FROM issue
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: MaterializeIssueChannelMediaMarkdown :one
-- Detached channel media resolves after /issue creation. When the description
-- still equals the exact creation-time base, replace its inline placeholders
-- with the fully composed Markdown so rich-text ordering survives. If a user
-- edited concurrently (or the adapter has no inline layout), append instead;
-- preserving user-authored bytes takes precedence over layout fidelity.
-- This is asynchronous system materialization, not a new user action, so it
-- intentionally preserves last_activity_at while still advancing revision.
UPDATE issue
SET description = CASE
        WHEN sqlc.narg('base_description')::text IS NOT NULL
             AND COALESCE(description, '') = sqlc.narg('base_description')::text
            THEN sqlc.arg('description')::text
        WHEN description IS NULL OR description = '' THEN sqlc.arg(markdown)
        ELSE description || E'\n\n' || sqlc.arg(markdown)
    END,
    revision = revision + 1,
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: LockIssueForDelete :one
-- Issue deletion must collect every attachment URL after it has won the same
-- row-lock race used by channel media binding. FOR UPDATE conflicts with the
-- binder's FOR KEY SHARE: either bind commits first and its URL is collected,
-- or delete commits first and the binder leaves its durable intent for cleanup.
SELECT id FROM issue
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: DetachDirectChildIssues :many
UPDATE issue
SET parent_issue_id = NULL,
    stage = NULL,
    revision = revision + 1,
    updated_at = now(),
    last_activity_at = GREATEST(COALESCE(last_activity_at, updated_at), now())
WHERE workspace_id = sqlc.arg(workspace_id)
  AND parent_issue_id = sqlc.arg(parent_issue_id)
  AND NOT COALESCE(id = ANY(sqlc.arg(excluded_issue_ids)::uuid[]), false)
RETURNING *;

-- name: CreateIssue :one
INSERT INTO issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    stage, last_activity_at, id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    sqlc.narg('stage'), now(), COALESCE(sqlc.narg('id')::uuid, gen_random_uuid())
) RETURNING *;

-- name: GetIssueByNumber :one
SELECT * FROM issue
WHERE workspace_id = $1 AND number = $2;

-- name: UpdateIssue :one
WITH candidate AS (
    SELECT
        i.*,
        COALESCE(sqlc.narg('title')::text, i.title) AS next_title,
        COALESCE(sqlc.narg('description')::text, i.description) AS next_description,
        COALESCE(sqlc.narg('status')::text, i.status) AS next_status,
        COALESCE(sqlc.narg('priority')::text, i.priority) AS next_priority,
        sqlc.narg('assignee_type')::text AS next_assignee_type,
        sqlc.narg('assignee_id')::uuid AS next_assignee_id,
        CASE
            -- An explicit position wins. Cross-column drag-and-drop sends
            -- status and position together and means the slot it dropped on.
            WHEN sqlc.narg('position')::double precision IS NOT NULL
                THEN sqlc.narg('position')::double precision
            -- position ranks an issue *within* its (workspace, status)
            -- column, so it stops meaning anything the moment the column
            -- changes: the value that put the issue on top of Todo lands it
            -- below every hand-dragged issue in Done. Re-rank to the top of
            -- the destination, the same policy new issues get from
            -- NextTopPosition. Keep the two in sync.
            --
            -- Two status changes running at once can read the same MIN and
            -- claim one slot. Every position-ordered query carries a unique
            -- final key (created_at DESC, id DESC), so a tie leaves the
            -- relative order of simultaneous moves arbitrary but never
            -- unstable across pages. Creation avoids the tie by computing its
            -- min under the workspace counter lock; a status change holds no
            -- such lock and is not worth taking one for.
            WHEN i.status IS DISTINCT FROM COALESCE(sqlc.narg('status')::text, i.status)
                THEN (
                    SELECT COALESCE(MIN(target.position), 0) - 1
                    FROM issue AS target
                    WHERE target.workspace_id = i.workspace_id
                      AND target.status = sqlc.narg('status')::text
                )
            ELSE i.position
        END AS next_position,
        sqlc.narg('start_date')::date AS next_start_date,
        sqlc.narg('due_date')::date AS next_due_date,
        sqlc.narg('parent_issue_id')::uuid AS next_parent_issue_id,
        sqlc.narg('project_id')::uuid AS next_project_id,
        sqlc.narg('stage')::integer AS next_stage
    FROM issue AS i
    WHERE i.id = $1
      AND (sqlc.narg('expected_revision')::bigint IS NULL OR i.revision = sqlc.narg('expected_revision')::bigint)
), changed AS (
    SELECT
        candidate.*,
        ROW(
            title, description, status, priority, assignee_type, assignee_id,
            position, start_date, due_date, parent_issue_id, project_id, stage
        ) IS DISTINCT FROM ROW(
            next_title, next_description, next_status, next_priority,
            next_assignee_type, next_assignee_id, next_position, next_start_date,
            next_due_date, next_parent_issue_id, next_project_id, next_stage
        ) AS did_change,
        ROW(
            title, description, status, priority, assignee_type, assignee_id,
            start_date, due_date, parent_issue_id, project_id, stage
        ) IS DISTINCT FROM ROW(
            next_title, next_description, next_status, next_priority,
            next_assignee_type, next_assignee_id, next_start_date, next_due_date,
            next_parent_issue_id, next_project_id, next_stage
        ) AS did_activity
    FROM candidate
)
UPDATE issue AS i SET
    title = changed.next_title,
    description = changed.next_description,
    status = changed.next_status,
    priority = changed.next_priority,
    assignee_type = changed.next_assignee_type,
    assignee_id = changed.next_assignee_id,
    position = changed.next_position,
    start_date = changed.next_start_date,
    due_date = changed.next_due_date,
    parent_issue_id = changed.next_parent_issue_id,
    project_id = changed.next_project_id,
    stage = changed.next_stage,
    revision = i.revision + changed.did_change::integer,
    last_activity_at = CASE WHEN changed.did_activity
        THEN GREATEST(COALESCE(i.last_activity_at, i.updated_at), now())
        ELSE i.last_activity_at
    END,
    updated_at = CASE WHEN changed.did_change THEN now() ELSE i.updated_at END
FROM changed
WHERE i.id = changed.id
  -- Re-check the precondition on the row version that UPDATE actually locks.
  -- Under READ COMMITTED, concurrent statements may both populate candidate
  -- from the same snapshot; EvalPlanQual re-evaluates this target-row predicate
  -- after waiting for the first writer, leaving the stale writer with 0 rows.
  AND (sqlc.narg('expected_revision')::bigint IS NULL OR i.revision = sqlc.narg('expected_revision')::bigint)
RETURNING i.*;

-- name: UpdateIssueStatus :one
-- Workspace_id in the WHERE clause is a SQL-layer tenant guard; see DeleteIssue.
-- Repositioning lives here rather than in the callers (GitHub sync, agent task
-- completion) so a status write cannot land without one: an issue carrying its
-- old column's rank into a new column is the bug this guards against. See the
-- next_position CASE in UpdateIssue for the policy.
UPDATE issue AS i SET
    status = $2,
    position = CASE WHEN i.status IS DISTINCT FROM $2 THEN (
        SELECT COALESCE(MIN(target.position), 0) - 1
        FROM issue AS target
        WHERE target.workspace_id = i.workspace_id
          AND target.status = $2
    ) ELSE i.position END,
    revision = i.revision + CASE WHEN i.status IS DISTINCT FROM $2 THEN 1 ELSE 0 END,
    last_activity_at = CASE WHEN i.status IS DISTINCT FROM $2
        THEN GREATEST(COALESCE(i.last_activity_at, i.updated_at), now())
        ELSE i.last_activity_at
    END,
    updated_at = now()
WHERE i.id = $1 AND i.workspace_id = $3
RETURNING *;

-- name: CreateIssueWithOrigin :one
INSERT INTO issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    origin_type, origin_id, stage, last_activity_at, id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    sqlc.narg('origin_type'), sqlc.narg('origin_id'), sqlc.narg('stage'), now(), COALESCE(sqlc.narg('id')::uuid, gen_random_uuid())
) RETURNING *;

-- name: LockIssueDuplicateKey :exec
SELECT pg_advisory_xact_lock(hashtextextended($1::text, 0));

-- name: FindActiveDuplicateIssue :one
SELECT * FROM issue
WHERE workspace_id = $1
  -- Negate only known terminal keys so an unknown legacy key remains active.
  AND NOT (status = ANY(sqlc.arg('terminal_status_keys')::text[]))
  AND project_id IS NOT DISTINCT FROM sqlc.arg('project_id')::uuid
  AND parent_issue_id IS NOT DISTINCT FROM sqlc.arg('parent_issue_id')::uuid
  AND lower(btrim(regexp_replace(title, '[[:space:]]+', ' ', 'g'))) = sqlc.arg('normalized_title')
ORDER BY created_at ASC
LIMIT 1;

-- name: FindRecentAutopilotDuplicateIssue :one
SELECT i.* FROM issue i
WHERE i.workspace_id = $1
  -- Negate only known terminal keys so an unknown legacy key remains active.
  AND NOT (i.status = ANY(sqlc.arg('terminal_status_keys')::text[]))
  AND i.origin_type = 'autopilot'
  AND i.origin_id = $2
  AND i.project_id IS NOT DISTINCT FROM sqlc.arg('project_id')::uuid
  AND lower(btrim(regexp_replace(i.title, '[[:space:]]+', ' ', 'g'))) = sqlc.arg('normalized_title')
  AND i.created_at >= sqlc.arg('created_after')::timestamptz
  AND EXISTS (
    SELECT 1
    FROM autopilot_run r
    WHERE r.issue_id = i.id
      AND r.autopilot_id = i.origin_id
      AND r.status IN ('issue_created', 'running', 'completed')
  )
ORDER BY i.created_at ASC
LIMIT 1;

-- name: DeleteIssue :exec
-- Defense-in-depth: the workspace_id predicate makes the tenant invariant a
-- SQL-layer guarantee rather than a handler-layer one. Handler loaders
-- (loadIssueForUser / GetIssueInWorkspace) already enforce membership today,
-- but a future loader bypass or a new caller skipping the loader would be
-- silently catastrophic without this guard. See incident #1661.
--
-- issue_vcs_pull_request (migration 213) has no FK to issue, so the link rows
-- are not cascaded away. Sweep them here so they go atomically with the issue.
-- The mirrored PR rows themselves belong to the connection, not the issue, so
-- they persist (matching the GitHub link behaviour).
--
-- The sweep MUST route through the same workspace-checked target as the issue
-- delete: deleting links by bare issue_id would drop another tenant's link rows
-- when a caller passes a foreign issue_id with its own workspace_id (the issue
-- itself is correctly untouched, but the links are already gone) — the exact
-- cross-tenant leak the #1661 guard above exists to prevent.
WITH target AS (
    SELECT issue.id FROM issue WHERE issue.id = $1 AND issue.workspace_id = $2
),
cleared_vcs_pr_links AS (
    DELETE FROM issue_vcs_pull_request WHERE issue_id IN (SELECT target.id FROM target)
)
DELETE FROM issue WHERE issue.id IN (SELECT target.id FROM target);

-- name: ListOpenIssues :many
-- See ListIssues for the semantics of involves_user_id (mirrors the 4-branch
-- filter; member-direct assignment is intentionally excluded).
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.last_activity_at, i.number, i.project_id, i.metadata, i.stage, i.properties,
       i.revision
FROM issue i
WHERE i.workspace_id = $1
  -- Negate only known terminal keys so an unknown legacy key remains visible.
  AND NOT (i.status = ANY(sqlc.arg('terminal_status_keys')::text[]))
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('metadata_filter')::jsonb IS NULL OR i.metadata @> sqlc.narg('metadata_filter')::jsonb)
  -- properties_filter is a jsonb array of groups, each group an array of
  -- containment patterns (built by parsePropertiesFilterParam): the issue
  -- must match at least one pattern from EVERY group (AND of ORs). A pattern
  -- of the shape {"__none__": "<definitionId>"} is the "no value" marker and
  -- matches when the issue's properties are missing that key. The correlated
  -- form skips the GIN index, which is fine here: open_only is already an
  -- unpaginated workspace scan. The terminal-status predicate intentionally
  -- remains a filter rather than a positive index narrowing so unknown legacy
  -- status keys stay visible.
  AND (
    sqlc.narg('properties_filter')::jsonb IS NULL
    OR NOT EXISTS (
      SELECT 1
      FROM jsonb_array_elements(sqlc.narg('properties_filter')::jsonb) AS pf(alternatives)
      WHERE NOT EXISTS (
        SELECT 1
        FROM jsonb_array_elements(pf.alternatives) AS alt(pattern)
        WHERE (alt.pattern ? '__none__' AND NOT (i.properties ? (alt.pattern ->> '__none__')))
           OR (NOT (alt.pattern ? '__none__') AND i.properties @> alt.pattern)
      )
    )
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
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('scheduled')::bool IS NULL OR (i.start_date IS NOT NULL OR i.due_date IS NOT NULL))
  AND (sqlc.narg('metadata_filter')::jsonb IS NULL OR i.metadata @> sqlc.narg('metadata_filter')::jsonb)
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
-- Order by number ASC so sub-issues display in stable creation order
-- (oldest first), matching how a parent's plan reads top-to-bottom. The
-- position column is computed per-(workspace, status) by NextTopPosition,
-- not relative to siblings, so ordering by it interleaves children
-- unpredictably across batches and statuses; number is a per-workspace
-- monotonic counter and is sibling-stable.
SELECT * FROM issue
WHERE parent_issue_id = $1
ORDER BY number ASC;

-- name: ListChildrenByParents :many
-- Batched variant of ListChildIssues: returns all children for the given
-- parent set in one round trip. Used by Swimlane to avoid an N+1 fan-out
-- (one request per visible parent lane). Result is grouped client-side by
-- parent_issue_id; the workspace filter is also enforced so callers can't
-- enumerate children of parents in workspaces they don't belong to.
-- Within each parent, order by number ASC for the same sibling-stable
-- creation order as ListChildIssues.
SELECT * FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')
  AND parent_issue_id = ANY(sqlc.arg('parent_ids')::uuid[])
ORDER BY parent_issue_id, number ASC;

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
       COUNT(*) FILTER (WHERE status = ANY(sqlc.arg('terminal_status_keys')::text[]))::bigint AS done
FROM issue
WHERE workspace_id = $1
  AND parent_issue_id IS NOT NULL
GROUP BY parent_issue_id;

-- SearchIssues: moved to handler (dynamic SQL for multi-word search support).

-- name: SetIssueMetadataKey :one
-- Atomically sets a single key in the issue's metadata JSONB. The
-- workspace_id filter is the authorization gate — handler resolves the
-- issue first so this is also the tenant check.
UPDATE issue SET
    metadata = jsonb_set(metadata, ARRAY[sqlc.arg('key')::text], sqlc.arg('value')::jsonb),
    revision = revision + CASE WHEN metadata -> sqlc.arg('key')::text IS DISTINCT FROM sqlc.arg('value')::jsonb THEN 1 ELSE 0 END,
    last_activity_at = CASE
        WHEN metadata -> sqlc.arg('key')::text IS DISTINCT FROM sqlc.arg('value')::jsonb
        THEN GREATEST(COALESCE(last_activity_at, updated_at), now())
        ELSE last_activity_at
    END,
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteIssueMetadataKey :one
-- Atomically removes a single key from the issue's metadata JSONB.
-- Deleting a missing key is a no-op (still returns the row).
UPDATE issue SET
    metadata = metadata - sqlc.arg('key')::text,
    revision = revision + CASE WHEN metadata ? sqlc.arg('key')::text THEN 1 ELSE 0 END,
    last_activity_at = CASE
        WHEN metadata ? sqlc.arg('key')::text
        THEN GREATEST(COALESCE(last_activity_at, updated_at), now())
        ELSE last_activity_at
    END,
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: MarkIssueFirstExecuted :one
-- Flips first_executed_at from NULL to now() atomically. Returns the row if
-- this was the first time the issue was executed; no rows otherwise. The
-- analytics issue_executed event fires exactly when this returns a row —
-- retries and re-assignments hit the WHERE clause and no-op.
UPDATE issue
SET first_executed_at = now()
WHERE id = $1 AND first_executed_at IS NULL
RETURNING id, workspace_id, creator_type, creator_id, first_executed_at;

-- name: CountIssuesUpTo :one
-- Bounded count for issue-limit admission and display. Callers pass only the
-- threshold needed for their decision, avoiding a full scan in an oversized
-- workspace.
SELECT COUNT(*)::bigint
FROM (
    SELECT 1
    FROM issue
    WHERE workspace_id = $1
    LIMIT sqlc.arg('limit')::bigint
) bounded_issues;
