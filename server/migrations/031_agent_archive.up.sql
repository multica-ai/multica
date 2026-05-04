-- Add archive support to agents (soft-delete replacement).
-- archived_at IS NOT NULL means the agent is archived.
ALTER TABLE agent ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ DEFAULT NULL;
ALTER TABLE agent ADD COLUMN IF NOT EXISTS archived_by UUID DEFAULT NULL REFERENCES "user"(id);
