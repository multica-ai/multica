-- name: EnsureDefaultIssueLifecycle :one
INSERT INTO issue_lifecycle (workspace_id, scope_type, scope_id, name)
VALUES ($1, 'workspace', $1, 'Default')
ON CONFLICT (workspace_id, scope_type, scope_id)
DO UPDATE SET name = issue_lifecycle.name
RETURNING *;

-- name: SetWorkspaceDefaultIssueLifecycle :exec
UPDATE workspace
SET default_issue_lifecycle_id = $2
WHERE id = $1
  AND default_issue_lifecycle_id IS DISTINCT FROM $2;

-- name: SyncDefaultIssueLifecycleStatuses :exec
INSERT INTO issue_lifecycle_status (
    workspace_id, lifecycle_id, legacy_status_key, name, description, color,
    position, phase, outcome, archived_at, created_at, updated_at
)
SELECT
    s.workspace_id,
    sqlc.arg('lifecycle_id')::uuid,
    s.key,
    s.name,
    s.description,
    s.color,
    s.position,
    CASE s.category
        WHEN 'backlog' THEN 'backlog'
        WHEN 'todo' THEN 'unstarted'
        WHEN 'done' THEN 'completed'
        WHEN 'cancelled' THEN 'cancelled'
        ELSE 'started'
    END,
    CASE s.category
        WHEN 'done' THEN 'completed'
        WHEN 'cancelled' THEN 'cancelled'
        ELSE NULL
    END,
    s.archived_at,
    s.created_at,
    s.updated_at
FROM issue_status AS s
WHERE s.workspace_id = sqlc.arg('workspace_id')::uuid
ON CONFLICT (lifecycle_id, legacy_status_key) WHERE legacy_status_key IS NOT NULL
DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    color = EXCLUDED.color,
    position = EXCLUDED.position,
    phase = EXCLUDED.phase,
    outcome = EXCLUDED.outcome,
    archived_at = EXCLUDED.archived_at,
    updated_at = EXCLUDED.updated_at;

-- name: GetDefaultIssueLifecycle :one
SELECT l.*
FROM workspace AS w
JOIN issue_lifecycle AS l ON l.id = w.default_issue_lifecycle_id
WHERE w.id = $1
  AND l.workspace_id = w.id;

-- name: GetEffectiveIssueLifecycle :one
SELECT l.*
FROM workspace AS w
LEFT JOIN project AS p
  ON p.id = sqlc.narg('project_id')::uuid
 AND p.workspace_id = w.id
JOIN issue_lifecycle AS l
  ON l.id = COALESCE(p.default_issue_lifecycle_id, w.default_issue_lifecycle_id)
 AND l.workspace_id = w.id
WHERE w.id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.narg('project_id')::uuid IS NULL OR p.id IS NOT NULL);

-- name: EnsureProjectIssueLifecycle :one
INSERT INTO issue_lifecycle (workspace_id, scope_type, scope_id, name)
SELECT p.workspace_id, 'project', p.id, p.title
FROM project AS p
WHERE p.id = sqlc.arg('project_id')::uuid
  AND p.workspace_id = sqlc.arg('workspace_id')::uuid
ON CONFLICT (workspace_id, scope_type, scope_id)
DO UPDATE SET name = issue_lifecycle.name
RETURNING *;

-- name: CloneIssueLifecycleStatuses :execrows
INSERT INTO issue_lifecycle_status (
    workspace_id, lifecycle_id, legacy_status_key, name, description, color,
    position, phase, outcome, entry_policy, entry_policy_revision, archived_at,
    created_at, updated_at
)
SELECT
    sqlc.arg('workspace_id')::uuid,
    sqlc.arg('target_lifecycle_id')::uuid,
    source.legacy_status_key,
    source.name,
    source.description,
    source.color,
    source.position,
    source.phase,
    source.outcome,
    source.entry_policy,
    source.entry_policy_revision,
    source.archived_at,
    source.created_at,
    source.updated_at
FROM issue_lifecycle_status AS source
WHERE source.workspace_id = sqlc.arg('workspace_id')::uuid
  AND source.lifecycle_id = sqlc.arg('source_lifecycle_id')::uuid
ON CONFLICT (lifecycle_id, legacy_status_key)
    WHERE legacy_status_key IS NOT NULL
DO NOTHING;

-- name: SetProjectIssueLifecycle :one
UPDATE project AS p
SET default_issue_lifecycle_id = l.id,
    updated_at = now()
FROM issue_lifecycle AS l
WHERE p.id = sqlc.arg('project_id')::uuid
  AND p.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.id = sqlc.arg('lifecycle_id')::uuid
  AND l.workspace_id = p.workspace_id
  AND l.scope_type = 'project'
  AND l.scope_id = p.id
RETURNING p.*;

-- name: ClearProjectIssueLifecycle :one
UPDATE project
SET default_issue_lifecycle_id = NULL,
    updated_at = now()
WHERE id = sqlc.arg('project_id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: GetIssueLifecycleByID :one
SELECT *
FROM issue_lifecycle
WHERE id = $1 AND workspace_id = $2;

-- name: LockEditableIssueLifecycle :one
-- Every custom-definition mutation takes this row lock first. Besides
-- serializing the lifecycle revision, the active-pointer predicate prevents
-- editing a project definition after that project switched back to the
-- workspace default. Workspace-default fields continue to be maintained by
-- the legacy status adapter until the final cutover, avoiding two writers.
SELECT l.*
FROM issue_lifecycle AS l
WHERE l.id = sqlc.arg('lifecycle_id')::uuid
  AND l.workspace_id = sqlc.arg('workspace_id')::uuid
  AND l.scope_type = 'project'
  AND EXISTS (
      SELECT 1 FROM project AS p
      WHERE p.id = l.scope_id
        AND p.workspace_id = l.workspace_id
        AND p.default_issue_lifecycle_id = l.id
  )
FOR UPDATE;

-- name: BumpIssueLifecycleRevision :one
UPDATE issue_lifecycle
SET revision = revision + 1,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
RETURNING *;

-- name: GetIssueLifecycleStatusByLegacyKey :one
SELECT *
FROM issue_lifecycle_status
WHERE workspace_id = $1
  AND lifecycle_id = $2
  AND legacy_status_key = $3;

-- name: GetIssueLifecycleStatusByID :one
SELECT *
FROM issue_lifecycle_status
WHERE workspace_id = $1
  AND lifecycle_id = $2
  AND id = $3;

-- name: UpdateIssueLifecycleStatusDefinition :one
UPDATE issue_lifecycle_status
SET name = sqlc.arg('name')::text,
    description = sqlc.arg('description')::text,
    color = sqlc.arg('color')::text,
    position = sqlc.arg('position')::double precision,
    phase = sqlc.arg('phase')::text,
    outcome = sqlc.narg('outcome')::text,
    entry_policy = sqlc.arg('entry_policy')::jsonb,
    entry_policy_revision = entry_policy_revision + CASE
        WHEN sqlc.arg('bump_entry_policy_revision')::boolean THEN 1 ELSE 0
    END,
    updated_at = now()
WHERE id = sqlc.arg('status_id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND lifecycle_id = sqlc.arg('lifecycle_id')::uuid
  AND archived_at IS NULL
RETURNING *;

-- name: ArchiveIssueLifecycleStatus :one
UPDATE issue_lifecycle_status
SET archived_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('status_id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
  AND lifecycle_id = sqlc.arg('lifecycle_id')::uuid
  AND archived_at IS NULL
RETURNING *;

-- name: ListActiveIssueLifecycleStatuses :many
SELECT *
FROM issue_lifecycle_status
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND lifecycle_id = sqlc.arg('lifecycle_id')::uuid
  AND archived_at IS NULL
ORDER BY position ASC, created_at ASC, id ASC;

-- name: ReorderIssueLifecycleStatuses :execrows
UPDATE issue_lifecycle_status AS status
SET position = ordered.ordinality - 1,
    updated_at = now()
FROM unnest(sqlc.arg('status_ids')::uuid[]) WITH ORDINALITY AS ordered(id, ordinality)
WHERE status.id = ordered.id
  AND status.workspace_id = sqlc.arg('workspace_id')::uuid
  AND status.lifecycle_id = sqlc.arg('lifecycle_id')::uuid
  AND status.archived_at IS NULL;

-- name: ListIssueLifecycleStatuses :many
SELECT *
FROM issue_lifecycle_status
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND lifecycle_id = sqlc.arg('lifecycle_id')::uuid
  AND (sqlc.arg('include_archived')::boolean OR archived_at IS NULL)
ORDER BY position ASC, created_at ASC, id ASC;

-- name: CountIssueLifecycleStatuses :one
SELECT count(*)::bigint
FROM issue_lifecycle_status
WHERE workspace_id = $1
  AND lifecycle_id = $2;

-- name: BindIssueToDefaultLifecycle :one
UPDATE issue AS i
SET lifecycle_id = w.default_issue_lifecycle_id,
    lifecycle_status_id = s.id
FROM workspace AS w
JOIN issue_lifecycle_status AS s
  ON s.lifecycle_id = w.default_issue_lifecycle_id
WHERE i.id = sqlc.arg('issue_id')::uuid
  AND i.workspace_id = sqlc.arg('workspace_id')::uuid
  AND w.id = i.workspace_id
  AND s.workspace_id = i.workspace_id
  AND s.legacy_status_key = i.status
RETURNING i.*;

-- name: BindIssueToLifecycleStatus :one
UPDATE issue
SET lifecycle_id = sqlc.arg('lifecycle_id')::uuid,
    lifecycle_status_id = sqlc.arg('lifecycle_status_id')::uuid
WHERE id = sqlc.arg('issue_id')::uuid
  AND workspace_id = sqlc.arg('workspace_id')::uuid
RETURNING *;

-- name: UpdateIssueLifecycleStatus :one
UPDATE issue AS i
SET status = s.legacy_status_key,
    lifecycle_status_id = s.id,
    revision = i.revision + 1,
    updated_at = now()
FROM issue_lifecycle_status AS s
WHERE i.id = sqlc.arg('issue_id')::uuid
  AND i.workspace_id = sqlc.arg('workspace_id')::uuid
  AND s.id = sqlc.arg('lifecycle_status_id')::uuid
  AND s.workspace_id = i.workspace_id
  AND s.lifecycle_id = i.lifecycle_id
  AND s.legacy_status_key IS NOT NULL
  AND s.archived_at IS NULL
RETURNING i.*;

-- name: InsertIssueTransition :execrows
INSERT INTO issue_transition (
    id, workspace_id, issue_id, lifecycle_id, lifecycle_revision,
    from_status_id, to_status_id, actor_type, actor_id, cause,
    issue_revision_before, issue_revision_after
)
VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12
)
ON CONFLICT (issue_id, issue_revision_after) DO NOTHING;

-- name: GetIssueTransitionByRevision :one
SELECT * FROM issue_transition
WHERE issue_id = $1
  AND issue_revision_after = $2;

-- name: SetIssueLastTransition :one
UPDATE issue
SET last_transition_id = $3
WHERE id = $1
  AND workspace_id = $2
RETURNING *;

-- name: GetIssueTransition :one
SELECT * FROM issue_transition
WHERE id = $1 AND workspace_id = $2;

-- name: ListIssueTransitions :many
SELECT * FROM issue_transition
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY created_at DESC, id DESC;

-- name: GetIssueLifecycleConsistency :one
SELECT
    CASE WHEN w.default_issue_lifecycle_id IS NULL THEN 1 ELSE 0 END::bigint AS workspaces_without_default,
    (
        SELECT count(*)::bigint
        FROM issue AS i
        WHERE i.workspace_id = w.id
          AND (i.lifecycle_id IS NULL OR i.lifecycle_status_id IS NULL)
    ) AS issues_without_binding,
    (
        SELECT count(*)::bigint
        FROM issue AS i
        LEFT JOIN issue_lifecycle_status AS s
          ON s.id = i.lifecycle_status_id
         AND s.lifecycle_id = i.lifecycle_id
         AND s.workspace_id = i.workspace_id
        WHERE i.workspace_id = w.id
          AND i.lifecycle_status_id IS NOT NULL
          AND (s.id IS NULL OR s.legacy_status_key IS DISTINCT FROM i.status)
    ) AS issues_with_status_mismatch,
    (
        SELECT count(*)::bigint
        FROM issue AS i
        LEFT JOIN issue_transition AS t
          ON t.id = i.last_transition_id
         AND t.workspace_id = i.workspace_id
        WHERE i.workspace_id = w.id
          AND (
              i.last_transition_id IS NULL OR
              t.id IS NULL OR
              t.issue_id IS DISTINCT FROM i.id OR
              t.lifecycle_id IS DISTINCT FROM i.lifecycle_id OR
              t.to_status_id IS DISTINCT FROM i.lifecycle_status_id OR
              t.issue_revision_after > i.revision
          )
    ) AS issues_with_transition_mismatch
FROM workspace AS w
WHERE w.id = $1;

-- name: ListIssueLifecycleConsistency :many
SELECT
    w.id AS workspace_id,
    CASE WHEN w.default_issue_lifecycle_id IS NULL THEN 1 ELSE 0 END::bigint AS workspaces_without_default,
    (
        SELECT count(*)::bigint
        FROM issue AS i
        WHERE i.workspace_id = w.id
          AND (i.lifecycle_id IS NULL OR i.lifecycle_status_id IS NULL)
    ) AS issues_without_binding,
    (
        SELECT count(*)::bigint
        FROM issue AS i
        LEFT JOIN issue_lifecycle_status AS s
          ON s.id = i.lifecycle_status_id
         AND s.lifecycle_id = i.lifecycle_id
         AND s.workspace_id = i.workspace_id
        WHERE i.workspace_id = w.id
          AND i.lifecycle_status_id IS NOT NULL
          AND (s.id IS NULL OR s.legacy_status_key IS DISTINCT FROM i.status)
    ) AS issues_with_status_mismatch,
    (
        SELECT count(*)::bigint
        FROM issue AS i
        LEFT JOIN issue_transition AS t
          ON t.id = i.last_transition_id
         AND t.workspace_id = i.workspace_id
        WHERE i.workspace_id = w.id
          AND (
              i.last_transition_id IS NULL OR
              t.id IS NULL OR
              t.issue_id IS DISTINCT FROM i.id OR
              t.lifecycle_id IS DISTINCT FROM i.lifecycle_id OR
              t.to_status_id IS DISTINCT FROM i.lifecycle_status_id OR
              t.issue_revision_after > i.revision
          )
    ) AS issues_with_transition_mismatch
FROM workspace AS w
ORDER BY w.id;
