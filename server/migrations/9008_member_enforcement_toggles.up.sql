-- Per-member toggles for the two enforcement axes shipped in JEH-324
-- (per-task scoped MULTICA_TOKEN) and JEH-327 (budget caps). Both default
-- TRUE so existing workspaces stay locked down; an admin can opt a single
-- member out from the new Users page.
--
--   scope_enforcement_enabled = false
--     mintTaskToken returns the daemon's own broad token instead of a
--     per-task mtt_. Used for trusted members whose runtime needs the
--     wider PAT scope (e.g. development).
--
--   budget_enforcement_enabled = false
--     CheckPreClaim skips the per-user daily/monthly cap for tasks
--     triggered by this member. Workspace hard kill still applies.

ALTER TABLE member
    ADD COLUMN scope_enforcement_enabled  BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN budget_enforcement_enabled BOOLEAN NOT NULL DEFAULT TRUE;

-- Track which user triggered each task. NULL when the trigger was a
-- system event (autopilot tick, scheduled job) or another agent — those
-- have no human user to attribute spend or scope to. ON DELETE SET NULL
-- so removing a member doesn't cascade-delete their task history.
ALTER TABLE agent_task_queue
    ADD COLUMN triggered_by_user_id UUID REFERENCES "user"(id) ON DELETE SET NULL;

CREATE INDEX idx_agent_task_queue_triggered_by
    ON agent_task_queue(triggered_by_user_id)
    WHERE triggered_by_user_id IS NOT NULL;

-- Extend budget_change_log so toggle flips are auditable alongside
-- numeric cap changes. The original CHECK constraint was scope_type
-- IN ('workspace_default','user_override','agent_override','workspace_safety');
-- replace it with a superset that includes the two new toggle scopes.
ALTER TABLE budget_change_log DROP CONSTRAINT IF EXISTS budget_change_log_scope_type_check;
ALTER TABLE budget_change_log
    ADD CONSTRAINT budget_change_log_scope_type_check
    CHECK (scope_type IN (
        'workspace_default',
        'user_override',
        'agent_override',
        'workspace_safety',
        'member_scope_toggle',
        'member_budget_toggle'
    ));
