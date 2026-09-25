-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction or a multi-command string.
--
-- Dispatch resolves an autopilot's ACTIVE rule version as "the newest row for this
-- (workspace, autopilot)", so index on (workspace_id, autopilot_id, created_at DESC)
-- to serve GetActiveAutopilotRuleVersion's ORDER BY created_at DESC LIMIT 1 without
-- a scan as the append-only table grows.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_autopilot_rule_version_active
    ON autopilot_rule_version (workspace_id, autopilot_id, created_at DESC);
