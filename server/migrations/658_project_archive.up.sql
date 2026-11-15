-- MUL-8861: archiving hides a project from the default lists, pickers and
-- agent project resolution without touching its status or its issues.
-- NULL means not archived, so existing rows need no backfill and older
-- servers keep writing valid rows. Nullable with no default, so the ALTER is
-- catalog-only. The archiving actor can be a member or an agent.
ALTER TABLE project
    ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS archived_by_type TEXT,
    ADD COLUMN IF NOT EXISTS archived_by_id UUID;
