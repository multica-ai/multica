-- TECH-3738 Bid C — capability drift watcher.
--
-- The watcher periodically checks each agent for capability DRIFT: a tool the
-- agent actually used (observed, Bid B) that its declared policy does not allow
-- (blocked or unmapped). When drift is found it alerts the workspace's
-- owners/admins. It is gated by the cerebro_capability_drift_watcher flag, which
-- defaults OFF, so the watcher does nothing until an admin turns it on.

-- name: ListWorkspacesWithCapabilityDriftWatcher :many
-- Workspace IDs where the drift watcher flag is ON at the workspace level
-- (user_id = all-zero sentinel = the workspace-level override row). Never-set =
-- absent = default OFF, so the watcher simply skips the workspace.
SELECT workspace_id FROM cerebro_feature_flags
WHERE user_id = '00000000-0000-0000-0000-000000000000'
  AND flag_key = 'cerebro_capability_drift_watcher'
  AND enabled = TRUE;

-- name: ListCerebroWorkspaceOwnerAdmins :many
-- The user IDs of every owner/admin in the workspace — the recipients of a
-- drift alert (drift is a security signal only admins can act on). Owners first.
SELECT user_id FROM member
WHERE workspace_id = sqlc.arg(workspace_id)
  AND role IN ('owner', 'admin')
ORDER BY (role = 'owner') DESC, created_at ASC;
