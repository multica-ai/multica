-- Fork-specific (FIR-1587): per-skill notification controls owned by the skill
-- owner. Closes the gap from FIR-1530 part 2 — notifications existed for change
-- requests but the owner had no on/off control, and forks / agent assignments
-- never notified at all.
--
--   * notify_change_requests — owner + approvers get an inbox item when a
--     change request is opened (default on).
--   * notify_forks — the parent-skill owner gets an inbox item when someone
--     forks the skill (default on).
--   * notify_agent_assigned — the owner gets an inbox item when an agent is
--     assigned the skill (default on).
--
-- All default TRUE so existing behaviour (change-request notifications) is
-- preserved and the two net-new streams start enabled, matching Jesper's
-- "default tændt for ændringsforslag og forks" requirement.

ALTER TABLE skill
    ADD COLUMN IF NOT EXISTS notify_change_requests BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS notify_forks BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS notify_agent_assigned BOOLEAN NOT NULL DEFAULT TRUE;
