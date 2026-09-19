ALTER TABLE agent
    DROP CONSTRAINT IF EXISTS agent_queued_ttl_seconds_valid;

ALTER TABLE agent
    DROP COLUMN IF EXISTS queued_ttl_seconds;
