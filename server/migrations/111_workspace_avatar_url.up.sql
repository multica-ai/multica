-- CEREBRO-PATCH(workspace-avatar-url-migration): FIR-2580 — add avatar_url column to workspace table.
-- PR #873 added handler references but omitted this migration, leaving CI broken.
-- PR #873 added avatar_url support to the workspace handler (CEREBRO-PATCH
-- workspace-avatar-*) but omitted this migration, leaving CI broken.
-- Nullable TEXT so existing workspaces have no avatar by default.
ALTER TABLE workspace
    ADD COLUMN IF NOT EXISTS avatar_url TEXT;
