-- Per-member budget enforcement toggle. Defaults TRUE so existing
-- workspaces stay locked down — an admin opts an individual member
-- out from the member detail page.
--
--   budget_enforcement_enabled = false
--     CheckPreClaim skips the per-user daily/monthly cap for tasks
--     triggered by this member. Workspace + agent caps unaffected.
ALTER TABLE member
    ADD COLUMN IF NOT EXISTS budget_enforcement_enabled BOOLEAN NOT NULL DEFAULT TRUE;
