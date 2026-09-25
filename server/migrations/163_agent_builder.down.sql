DROP INDEX IF EXISTS agent_system_identity_unique;

ALTER TABLE agent
DROP COLUMN IF EXISTS system_key,
DROP COLUMN IF EXISTS kind;
