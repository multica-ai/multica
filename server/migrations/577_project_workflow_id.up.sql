-- The workflow a project uses. NULL means the implicit Default workflow (the
-- whole issue_status catalog, no handoffs), which is what every existing
-- project keeps. Nullable with no default, so adding it is metadata-only.
ALTER TABLE project ADD COLUMN IF NOT EXISTS workflow_id UUID;
