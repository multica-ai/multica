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
