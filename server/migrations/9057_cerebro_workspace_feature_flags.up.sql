-- FIR-2505 — workspace-level feature-flag overrides.
--
-- Until now cerebro_feature_flags held only per-(workspace,user) personal
-- overrides. A workspace owner/admin now needs to force a flag on (or off)
-- for the WHOLE workspace such that individual members cannot toggle it back.
--
-- We model the workspace-level override as a row whose user_id is the
-- all-zero sentinel UUID (no real user owns it). The new `locked` column
-- distinguishes the two owner intents:
--   * locked = true  -> the workspace value wins and members cannot override
--                       it (the personal toggle is shown disabled).
--   * locked = false -> a soft workspace default that a member may still
--                       override with their personal toggle.
--
-- Per-user rows keep locked = false (unused on that path). No constraint
-- change is needed: (workspace_id, user_id, flag_key) already makes the
-- sentinel row unique per (workspace, flag), and the per-user List query
-- filters on the real user_id so it never picks up the sentinel row.
ALTER TABLE cerebro_feature_flags
    ADD COLUMN IF NOT EXISTS locked boolean NOT NULL DEFAULT false;
