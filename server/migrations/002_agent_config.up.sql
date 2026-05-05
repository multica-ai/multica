-- CEREBRO-PATCH(migration-idempotent-002-agent-config): cerebro modification of upstream file
-- Add agent configuration columns: skills, tools, triggers
ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS skills TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS tools JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS triggers JSONB NOT NULL DEFAULT '[]';
