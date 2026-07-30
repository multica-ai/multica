-- FIR-4012 — the workspace capability digest.
--
-- The drift watcher used to write one inbox card per agent, and an inbox card
-- cannot be answered. The digest replaces that with ONE issue per workspace,
-- reused while it is open, so the whole finding set is in one place and the
-- reader can reply on it. These queries are the persistence half; the fan-in
-- and rendering live in internal/cerebro/driftwatch/digest.go.

-- name: GetOpenCapabilityDigestIssue :one
-- The workspace's current digest issue. The sweeper stamps
-- (origin_type='capability_digest', origin_id=workspace_id) on creation so this
-- lookup dedupes in O(1). "Open" = status NOT IN ('done','cancelled'): once a
-- reader closes the digest, the next sweep opens a fresh one rather than
-- silently reviving an issue they already actioned.
SELECT * FROM issue
WHERE workspace_id = sqlc.arg(workspace_id)
  AND origin_type = 'capability_digest'
  AND origin_id = sqlc.arg(workspace_id)
  AND status NOT IN ('done', 'cancelled')
ORDER BY created_at DESC
LIMIT 1;

-- name: UpdateCapabilityDigestIssue :one
-- Rewrites the open digest with the latest finding set. Deliberately narrow:
-- only title, description and updated_at. The upstream UpdateIssue sets
-- assignee/parent/project/dates from nullable args, so calling it with an
-- unset assignee would silently unassign an issue a human had routed.
UPDATE issue SET
    title = sqlc.arg(title),
    description = sqlc.arg(description),
    updated_at = now()
WHERE id = sqlc.arg(id)
  AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: ListWorkspacesWithCapabilityAutoPermission :many
-- Workspace IDs where auto-creating a permission rule for an unmapped
-- capability is ON at the workspace level (user_id = all-zero sentinel = the
-- workspace-level override row). Never-set = absent = default OFF, so a
-- workspace that never opted in never gets a rule written on its behalf.
SELECT workspace_id FROM cerebro_feature_flags
WHERE user_id = '00000000-0000-0000-0000-000000000000'
  AND flag_key = 'cerebro_capability_auto_permission'
  AND enabled = TRUE;
