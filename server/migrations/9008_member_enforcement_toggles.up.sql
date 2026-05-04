-- Per-member enforcement toggles. Both default TRUE so existing
-- workspaces stay locked down — an admin opts an individual member
-- out from the member detail page.
--
--   scope_enforcement_enabled = false
--     mintTaskToken returns a wider 1h ScopeUser token instead of
--     the strict per-task mtt_ (JEH-324 admin opt-out).
--
--   budget_enforcement_enabled = false
--     CheckPreClaim skips the per-user daily/monthly cap for tasks
--     triggered by this member. Workspace + agent caps unaffected.
ALTER TABLE member
    ADD COLUMN scope_enforcement_enabled  BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN budget_enforcement_enabled BOOLEAN NOT NULL DEFAULT TRUE;
