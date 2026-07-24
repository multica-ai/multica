-- name: ListIssues :many
-- involves_user_id widens the assignee filter to surface issues where the user
-- is *indirectly* the assignee — via an owned agent or a squad they belong to /
-- lead / have an agent inside. The semantics intentionally exclude direct
-- member assignment (`assignee_type='member' AND assignee_id=involves_user_id`)
-- because that is already the meaning of the `assignee_id` filter (tab 1
-- "Assigned to me"), and the two filters must produce disjoint result sets.
SELECT i.id, i.workspace_id, i.title, i.description, i.status, i.priority,
       i.assignee_type, i.assignee_id, i.creator_type, i.creator_id,
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id, i.metadata, i.stage, i.properties
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

-- name: CreateIssue :one
INSERT INTO issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    stage, status_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    sqlc.narg('stage'),
    -- Phase 2 double-write (MUL-4809 §6.1). status_id is supplied explicitly by
    -- the caller, which resolved it through issuestatus.ResolveForWrite and
    -- derived the legacy `status` above from the SAME row. NULL while the
    -- workspace catalog is unseeded, where status stays the source of truth.
    sqlc.narg('status_id')
) RETURNING *;

-- name: GetIssueByNumber :one
SELECT * FROM issue
WHERE workspace_id = $1 AND number = $2;

-- name: UpdateIssue :one
UPDATE issue SET
    title = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status = COALESCE(sqlc.narg('status'), status),
    -- Phase 2 double-write (MUL-4809 §6.1). status_id is supplied explicitly by
    -- the caller, which resolved it through issuestatus.Resolve and derived the
    -- compat `status` above from the SAME row — so the pair can never disagree.
    -- Deriving it here from system_key (as this once did) could only ever reach
    -- the 7 built-ins and made custom statuses unreachable.
    status_id = COALESCE(sqlc.narg('status_id'), status_id),
    priority = COALESCE(sqlc.narg('priority'), priority),
    assignee_type = sqlc.narg('assignee_type'),
    assignee_id = sqlc.narg('assignee_id'),
    position = COALESCE(sqlc.narg('position'), position),
    start_date = sqlc.narg('start_date'),
    due_date = sqlc.narg('due_date'),
    parent_issue_id = sqlc.narg('parent_issue_id'),
    project_id = sqlc.narg('project_id'),
    stage = sqlc.narg('stage'),
    updated_at = now()
WHERE issue.id = $1
RETURNING *;

-- name: UpdateIssueStatus :one
-- Internal Category-alias transition (failed-task reset, PR merge, stuck-issue
-- sweep). Workspace_id in the WHERE clause is a SQL-layer tenant guard; see
-- DeleteIssue.
--
-- $2 is a token: a Category alias (backlog/todo/in_progress/done/cancelled) or a
-- legacy alias (in_review/blocked). A Category alias MUST resolve to the
-- workspace's CURRENT default status in that Category — which may be a custom
-- status an admin promoted — not the built-in by system_key; otherwise these
-- paths silently bypass the configured workflow (MUL-4809 §3.1). Legacy aliases
-- resolve to the built-in carrying that system_key. Both status_id and the
-- legacy `status` projection are written from the SAME resolved row, in one
-- atomic statement, and only ever an ACTIVE status. An unseeded workspace
-- resolves to nothing → status_id stays NULL and the bare token is kept.
WITH resolved AS (
    SELECT s.id, COALESCE(s.system_key, s.category) AS token
    FROM issue_status s
    WHERE s.workspace_id = $3 AND s.archived_at IS NULL
      AND CASE
        WHEN $2 IN ('backlog', 'todo', 'in_progress', 'done', 'cancelled')
          THEN s.category = $2 AND s.is_default
        ELSE s.system_key = $2
      END
    LIMIT 1
)
UPDATE issue SET
    status = COALESCE((SELECT token FROM resolved), $2),
    status_id = (SELECT id FROM resolved),
    updated_at = now()
WHERE issue.id = $1 AND issue.workspace_id = $3
RETURNING *;

-- name: CreateIssueWithOrigin :one
INSERT INTO issue (
    workspace_id, title, description, status, priority,
    assignee_type, assignee_id, creator_type, creator_id,
    parent_issue_id, position, start_date, due_date, number, project_id,
    origin_type, origin_id, stage, status_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
    sqlc.narg('origin_type'), sqlc.narg('origin_id'), sqlc.narg('stage'),
    -- Phase 2 double-write (MUL-4809 §6.1): see CreateIssue.
    sqlc.narg('status_id')
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

-- name: FindRecentAutopilotDuplicateIssue :one
SELECT i.* FROM issue i
WHERE i.workspace_id = $1
  AND i.status NOT IN ('done', 'cancelled')
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
       i.parent_issue_id, i.position, i.start_date, i.due_date, i.created_at, i.updated_at, i.number, i.project_id, i.metadata, i.stage, i.properties
FROM issue i
WHERE i.workspace_id = $1
  AND i.status NOT IN ('done', 'cancelled')
  -- The three status predicates carry the same status_id IS NULL compat arm the
  -- dynamic ListIssues path uses (statusCatalogMatchSQL / statusCategoryMatchSQL).
  -- A workspace that upgraded before the one-shot backfill still has rows with
  -- status_id IS NULL; without this arm they vanish from open_only status
  -- filters, reading as data loss. Only a selected BUILT-IN claims a legacy row
  -- (system_key is 1:1 with the token); a custom status id never matches a NULL
  -- legacy row (MUL-4809).
  AND (sqlc.narg('status_id')::uuid IS NULL OR i.status_id = sqlc.narg('status_id')
        OR (i.status_id IS NULL AND i.status = (
              SELECT s.system_key FROM issue_status s
               WHERE s.id = sqlc.narg('status_id') AND s.workspace_id = i.workspace_id
                 AND s.system_key IS NOT NULL)))
  -- status_ids (MUL-4809): multi-select by catalog id — one board column per
  -- status, and multi-select filter chips. OR within the field.
  AND (sqlc.narg('status_ids')::uuid[] IS NULL OR i.status_id = ANY(sqlc.narg('status_ids')::uuid[])
        OR (i.status_id IS NULL AND i.status = ANY(
              SELECT s.system_key FROM issue_status s
               WHERE s.id = ANY(sqlc.narg('status_ids')::uuid[]) AND s.workspace_id = i.workspace_id
                 AND s.system_key IS NOT NULL)))
  -- status_category (MUL-4809): match issues whose status_id resolves to the
  -- given Category, scoped to the same workspace (no FK). Mirrors the EXISTS
  -- predicate the dynamic ListIssues/ListGroupedIssues paths build, plus the
  -- NULL-row arm: a legacy row is classified by projecting its token
  -- (in_review / blocked -> in_progress; else itself) to a Category.
  AND (sqlc.narg('status_category')::text IS NULL OR EXISTS (
        SELECT 1 FROM issue_status s
         WHERE s.id = i.status_id AND s.workspace_id = i.workspace_id
           AND s.category = sqlc.narg('status_category')::text)
        OR (i.status_id IS NULL AND
              CASE WHEN i.status IN ('in_review', 'blocked') THEN 'in_progress' ELSE i.status END
                = sqlc.narg('status_category')::text))
  AND (sqlc.narg('priority')::text IS NULL OR i.priority = sqlc.narg('priority'))
  AND (sqlc.narg('assignee_id')::uuid IS NULL OR i.assignee_id = sqlc.narg('assignee_id'))
  AND (sqlc.narg('assignee_ids')::uuid[] IS NULL OR i.assignee_id = ANY(sqlc.narg('assignee_ids')::uuid[]))
  AND (sqlc.narg('creator_id')::uuid IS NULL OR i.creator_id = sqlc.narg('creator_id'))
  AND (sqlc.narg('project_id')::uuid IS NULL OR i.project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('metadata_filter')::jsonb IS NULL OR i.metadata @> sqlc.narg('metadata_filter')::jsonb)
  -- properties_filter is a jsonb array of groups, each group an array of
  -- containment patterns (built by parsePropertiesFilterParam): the issue
  -- must match at least one pattern from EVERY group (AND of ORs). The
  -- correlated form skips the GIN index, which is fine here: open_only is
  -- an unpaginated workspace scan already narrowed by status.
  AND (
    sqlc.narg('properties_filter')::jsonb IS NULL
    OR NOT EXISTS (
      SELECT 1
      FROM jsonb_array_elements(sqlc.narg('properties_filter')::jsonb) AS pf(alternatives)
      WHERE NOT EXISTS (
        SELECT 1
        FROM jsonb_array_elements(pf.alternatives) AS alt(pattern)
        WHERE i.properties @> alt.pattern
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

-- name: StatusDetailsByIssues :many
-- Resolve the custom-status catalog detail for a batch of issues (MUL-4809 read
-- side). Join each issue to the issue_status it points at via the authoritative
-- status_id, scoped to the same workspace (no FK). Issues whose status_id is NULL
-- (workspace catalog not seeded) simply don't appear. Archived statuses are
-- included so an issue already on one still renders its detail.
SELECT i.id AS issue_id,
       s.id AS status_id,
       s.name,
       s.category,
       s.icon,
       s.color
FROM issue i
JOIN issue_status s ON s.id = i.status_id AND s.workspace_id = i.workspace_id
WHERE i.workspace_id = sqlc.arg('workspace_id')::uuid
  AND i.id = ANY(sqlc.arg('issue_ids')::uuid[]);

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
       COUNT(*) FILTER (WHERE status IN ('done', 'cancelled'))::bigint AS done
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
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteIssueMetadataKey :one
-- Atomically removes a single key from the issue's metadata JSONB.
-- Deleting a missing key is a no-op (still returns the row).
UPDATE issue SET
    metadata = metadata - sqlc.arg('key')::text,
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
