-- FIR-2596 — Google Workspace group sync (read from BigQuery).
--
-- Three additive changes, all backwards-compatible:
--
-- 1. Per-workspace on/off switch for the sync worker. Lives on the existing
--    Google-identity settings row so the Auth & Permissions tab owns the
--    whole Google integration. Default false = the worker never touches a
--    workspace until an owner/admin opts in.
--
-- 2. cerebro_group.google_group_email: the source-of-truth key the sync
--    reconciles against. Matching on the Google group EMAIL (not the display
--    name) means a synced group keeps its identity even if someone renames
--    it in the UI, and it never collides with a manually-created group that
--    happens to share a name. NULL for hand-made groups. Partial unique
--    index keeps one synced group per (workspace, google email).
--
-- 3. cerebro_group_member.source: provenance so reconciliation only ever
--    removes members it added itself. A member added by hand ('manual')
--    survives even if they are absent from Google; a member added by the
--    sync ('google_workspace') is removed when they leave the Google group.

ALTER TABLE cerebro_workspace_auth_settings
    ADD COLUMN IF NOT EXISTS google_workspace_sync_enabled BOOLEAN NOT NULL DEFAULT false;

ALTER TABLE cerebro_group
    ADD COLUMN IF NOT EXISTS google_group_email TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_group_google_email
    ON cerebro_group (workspace_id, google_group_email)
    WHERE google_group_email IS NOT NULL;

ALTER TABLE cerebro_group_member
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual';
