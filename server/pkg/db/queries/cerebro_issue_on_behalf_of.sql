-- CEREBRO-PATCH(issue-on-behalf-of-column): FIR-4930 — net-new cerebro queries for
-- the explicit `on_behalf_of_user_id` stamp added by migration
-- 9177_cerebro_issue_on_behalf_of. Upstream multica has no such file.

-- name: SetIssueOnBehalfOf :one
-- Stamps (or clears, when the arg is NULL) the human an agent acted for. The
-- workspace_id filter is the tenant guard — the handler has already resolved
-- and authorized the issue, this makes a cross-workspace write impossible.
UPDATE issue SET
    on_behalf_of_user_id = sqlc.narg('on_behalf_of_user_id'),
    updated_at = now()
WHERE id = sqlc.arg('id') AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: ClearStaleTriggeredAgentSubscribers :exec
-- Used when an issue's human origin is corrected: drops every auto-added
-- 'triggered_agent' subscriber except the one we are keeping, so the previously
-- attributed human stops receiving inbox notifications for work they don't own.
-- Only touches rows the platform added itself (reason='triggered_agent') —
-- a manually added subscriber has a different reason and survives.
DELETE FROM issue_subscriber
WHERE issue_id = sqlc.arg('issue_id')
  AND reason = 'triggered_agent'
-- IS NOT DISTINCT FROM (not `=`) so a NULL keep_user_id — the "stamp cleared"
-- case — deletes every triggered_agent row instead of silently deleting none.
  AND NOT (user_type = 'member' AND user_id IS NOT DISTINCT FROM sqlc.narg('keep_user_id')::uuid);
