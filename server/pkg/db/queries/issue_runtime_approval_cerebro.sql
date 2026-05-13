-- CEREBRO-PATCH(runtime-approval-issue): JEH-1152 — auto-create / reuse an
-- "Approve runtime" issue when a non-admin's agent-create gets gated on the
-- group runtime allowlist. Net-new file; query lives outside the upstream
-- issue.sql to keep the cerebro carve-out isolated.

-- name: GetOpenRuntimeApprovalIssue :one
-- Finds the existing open approval issue for (workspace_id, runtime_id). The
-- approval-issue helper stamps (origin_type='runtime_approval', origin_id=
-- runtime_id) on creation so this lookup can dedupe. "Open" = status NOT IN
-- ('done', 'cancelled'); reusing a cancelled/done issue would silently
-- revive a request the admin already actioned.
SELECT * FROM issue
WHERE workspace_id = $1
  AND origin_type = 'runtime_approval'
  AND origin_id = $2
  AND status NOT IN ('done', 'cancelled')
ORDER BY created_at DESC
LIMIT 1;
