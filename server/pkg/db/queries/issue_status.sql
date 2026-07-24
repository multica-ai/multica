-- name: EnsureWorkspaceSystemIssueStatuses :exec
-- Idempotently seed the 7 built-in system statuses for a workspace (MUL-4809).
--
-- The `WITH ws ... FOR KEY SHARE` clause is the no-FK integrity guard. It takes
-- the same lock the workspace delete/create protocol uses
-- (LockWorkspaceForChatSessionCreate), so a concurrent DeleteWorkspace (which
-- holds FOR UPDATE) cannot interleave: if the workspace row is already gone, ws
-- is empty and zero rows are inserted, so Backfill can never re-create orphan
-- statuses for a workspace deleted mid-walk. Callers need no separate existence
-- check. At workspace-create time the row is visible to the same transaction,
-- so the seed proceeds normally.
--
-- ON CONFLICT names the (workspace_id, system_key) partial unique index as the
-- EXPLICIT arbiter, so a re-run is a no-op while any OTHER unique violation
-- surfaces loudly instead of silently dropping a built-in.
--
-- category is the only machine semantics; icon (the frontend renders a bespoke
-- SVG keyed on the status) and color (a semantic UI token) match the current
-- hardcoded status visuals. is_default marks the Category alias target for the
-- five categories (in_review and blocked are non-default in_progress statuses).
WITH ws AS (
    SELECT id FROM workspace WHERE id = sqlc.arg('workspace_id')::uuid FOR KEY SHARE
)
INSERT INTO issue_status (
    workspace_id, name, description, icon, color, category, system_key, is_default, position
)
SELECT ws.id, v.name, v.description, v.icon, v.color, v.category, v.system_key, v.is_default, v.position
FROM ws
CROSS JOIN (VALUES
    ('Backlog',     '', 'backlog',     'muted-foreground', 'backlog',     'backlog',     TRUE,  0::double precision),
    ('Todo',        '', 'todo',        'muted-foreground', 'todo',        'todo',        TRUE,  0),
    ('In Progress', '', 'in_progress', 'warning',          'in_progress', 'in_progress', TRUE,  0),
    ('In Review',   '', 'in_review',   'success',          'in_progress', 'in_review',   FALSE, 1),
    ('Blocked',     '', 'blocked',     'destructive',      'in_progress', 'blocked',     FALSE, 2),
    ('Done',        '', 'done',        'info',             'done',        'done',        TRUE,  0),
    ('Cancelled',   '', 'cancelled',   'muted-foreground', 'cancelled',   'cancelled',   TRUE,  0)
) AS v(name, description, icon, color, category, system_key, is_default, position)
ON CONFLICT (workspace_id, system_key) WHERE system_key IS NOT NULL DO NOTHING;

-- name: ListWorkspaceIssueStatuses :many
-- Active statuses for a workspace, ordered by the fixed Category order then
-- intra-category position. Used by tests and (later phases) the catalog API.
SELECT * FROM issue_status
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND (sqlc.arg('include_archived')::bool OR archived_at IS NULL)
ORDER BY
    CASE category
        WHEN 'backlog' THEN 0
        WHEN 'todo' THEN 1
        WHEN 'in_progress' THEN 2
        WHEN 'done' THEN 3
        WHEN 'cancelled' THEN 4
    END,
    position ASC,
    created_at ASC;

-- name: CountWorkspaceIssueStatuses :one
-- Count of active statuses in a workspace (seed / backfill invariant checks).
SELECT COUNT(*) FROM issue_status
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND archived_at IS NULL;

-- ---------------------------------------------------------------------------
-- Status-management API (MUL-4809, plan §5). Admin CRUD over the catalog.
-- All writes are tenant-scoped by workspace_id (no FK; the WHERE clause is the
-- application-level guard) and run under a workspace advisory lock so the cap,
-- name-uniqueness, and single-default swaps stay atomic.
-- ---------------------------------------------------------------------------

-- name: GetWorkspaceIssueStatus :one
SELECT * FROM issue_status WHERE id = $1 AND workspace_id = $2;

-- name: CountActiveCustomIssueStatuses :one
-- Active custom (non-system) statuses, for the per-workspace cap. System
-- statuses never count against it — the 7 built-ins are always present.
SELECT COUNT(*) FROM issue_status
WHERE workspace_id = $1 AND system_key IS NULL AND archived_at IS NULL;

-- name: CreateCustomIssueStatus :one
-- Custom statuses always have system_key = NULL and append to the end of their
-- Category: position = max(position within category) + 1, so they sort after
-- the built-ins of the same Category.
--
-- The `WITH ws ... FOR KEY SHARE` clause is a defense-in-depth workspace
-- existence gate (mirrors EnsureWorkspaceSystemIssueStatuses). The PRIMARY gate
-- and lock order now live in LockWorkspaceForStatusWrite, which takes this same
-- FOR KEY SHARE on the workspace row at the very start of the transaction —
-- before any default-swap touches a status row — so the create/delete ordering
-- cannot deadlock. This CTE stays as a backstop: if the workspace row is gone,
-- ws is empty and zero rows are inserted (never an orphan), and the :one query
-- returns pgx.ErrNoRows, which the handler also maps to 404.
WITH ws AS (
    SELECT id FROM workspace WHERE id = sqlc.arg('workspace_id')::uuid FOR KEY SHARE
)
INSERT INTO issue_status (
    workspace_id, name, description, icon, color, category, system_key, is_default, position
)
SELECT ws.id,
       sqlc.arg('name')::text,
       sqlc.arg('description')::text,
       sqlc.arg('icon')::text,
       sqlc.arg('color')::text,
       sqlc.arg('category')::text,
       NULL::text,
       sqlc.arg('is_default')::bool,
       COALESCE((SELECT MAX(position) FROM issue_status
                 WHERE workspace_id = ws.id
                   AND category = sqlc.arg('category')::text), 0) + 1
FROM ws
RETURNING *;

-- name: UpdateIssueStatusFields :one
-- Mutable human-facing fields only. category / system_key / workspace_id are
-- immutable and never appear here (the handler rejects them with 400). is_default
-- is handled by the default-swap flow below, not this COALESCE update, so the
-- one-default-per-Category invariant can be maintained across rows in one tx.
UPDATE issue_status SET
    name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    icon = COALESCE(sqlc.narg('icon'), icon),
    color = COALESCE(sqlc.narg('color'), color),
    position = COALESCE(sqlc.narg('position'), position),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ClearCategoryDefault :exec
-- Drop is_default from every active status in a Category, so a new default can
-- be set without tripping the (workspace_id, category) partial unique index.
-- Run inside the same tx as the promote below (or a create-with-default).
UPDATE issue_status SET is_default = FALSE, updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND category = sqlc.arg('category')::text
  AND is_default = TRUE
  AND archived_at IS NULL;

-- name: SetIssueStatusDefault :one
UPDATE issue_status SET is_default = sqlc.arg('is_default')::bool, updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ArchiveIssueStatus :one
UPDATE issue_status SET archived_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: CountIssuesUsingStatus :one
-- Issues currently pointing at a status via the authoritative status_id.
SELECT COUNT(*) FROM issue WHERE workspace_id = $1 AND status_id = $2;

-- name: ReassignIssuesStatus :exec
-- Move every issue off one status onto another during archive migration. The
-- handler guarantees both are the same Category, so the legacy `status`
-- projection (Category for custom statuses) is unchanged and is left as-is.
UPDATE issue SET status_id = sqlc.arg('to_status_id')::uuid, updated_at = now()
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND status_id = sqlc.arg('from_status_id')::uuid;
