-- name: CreateIssueView :one
INSERT INTO issue_view (
    workspace_id, owner_id, name, scope_type, scope_id, scope_variant,
    visibility, definition_version, query, display
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: ListIssueViewsForUser :many
-- One surface's selector list: the caller's own views plus workspace-shared
-- ones, for a single (scope_type, scope_id) container. scope_id is NULL for
-- workspace and my scopes, so compare NULL-safely.
SELECT * FROM issue_view
WHERE workspace_id = $1
  AND scope_type = $2
  AND scope_id IS NOT DISTINCT FROM sqlc.narg('scope_id')::uuid
  AND (owner_id = $3 OR visibility = 'workspace')
ORDER BY created_at ASC
-- Hard response cap: the bar/panel are not built for more than this, and
-- every row carries full query/display JSON. The create quota keeps real
-- data far below it; the LIMIT is the abuse backstop.
LIMIT 200;

-- name: CountIssueViewsByOwner :one
SELECT COUNT(*) FROM issue_view
WHERE workspace_id = $1 AND owner_id = $2;

-- name: GetIssueView :one
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. Authorization
-- (owner vs shared) happens in the handler so unauthorized reads 404.
SELECT * FROM issue_view
WHERE id = $1 AND workspace_id = $2;

-- name: UpdateIssueView :one
-- Optimistic concurrency: the row only updates when the caller's revision
-- matches; zero rows back means either a stale revision (409) or a vanished
-- view (404) — the handler re-reads to tell them apart.
UPDATE issue_view SET
    name = $3,
    visibility = $4,
    scope_variant = $5,
    query = $6,
    display = $7,
    revision = revision + 1,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND revision = $8
RETURNING *;

-- name: DeleteIssueView :one
-- Every view delete sweeps its sidebar pins in the same statement: a pin
-- whose view is gone is invisible in the UI (view pins never auto-unpin)
-- and would otherwise be unremovable forever.
WITH deleted AS (
    DELETE FROM issue_view
    WHERE issue_view.id = $1 AND issue_view.workspace_id = $2
    RETURNING issue_view.id
),
swept_pins AS (
    DELETE FROM pinned_item
    WHERE pinned_item.item_type = 'view'
      AND pinned_item.workspace_id = $2
      AND pinned_item.item_id IN (SELECT deleted.id FROM deleted)
)
SELECT deleted.id FROM deleted;

-- name: DeleteIssueViewsByProjectScope :exec
-- Project deletion cleanup: called inside DeleteProject's application
-- transaction so project views never outlive their surface. Their sidebar
-- pins fall in the same statement (see DeleteIssueView).
WITH deleted AS (
    DELETE FROM issue_view
    WHERE issue_view.workspace_id = $1 AND issue_view.scope_type = 'project' AND issue_view.scope_id = $2
    RETURNING issue_view.id
)
DELETE FROM pinned_item
WHERE pinned_item.item_type = 'view'
  AND pinned_item.workspace_id = $1
  AND pinned_item.item_id IN (SELECT deleted.id FROM deleted);

-- name: DeletePrivateIssueViewsByOwner :exec
-- Member removal: a departed member's private views are unreachable by every
-- remaining member while still consuming their quota — same rule as private
-- quick actions. Shared views are workspace furniture and survive. Pins on
-- the deleted views fall in the same statement (see DeleteIssueView).
WITH deleted AS (
    DELETE FROM issue_view
    WHERE issue_view.workspace_id = $1 AND issue_view.owner_id = $2 AND issue_view.visibility = 'private'
    RETURNING issue_view.id
)
DELETE FROM pinned_item
WHERE pinned_item.item_type = 'view'
  AND pinned_item.workspace_id = $1
  AND pinned_item.item_id IN (SELECT deleted.id FROM deleted);

-- name: DeleteIssueViewPreferencesByUser :exec
DELETE FROM issue_view_preference
WHERE workspace_id = $1 AND user_id = $2;
