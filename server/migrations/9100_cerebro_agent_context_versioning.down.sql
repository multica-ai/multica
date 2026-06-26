-- Reverse 9100_cerebro_agent_context_versioning.

DROP TABLE IF EXISTS agent_change_request;
DROP TABLE IF EXISTS agent_context_version;

DROP INDEX IF EXISTS idx_agent_context_owner;

ALTER TABLE agent
    DROP COLUMN IF EXISTS context_owner_id,
    DROP COLUMN IF EXISTS context_approver_ids,
    DROP COLUMN IF EXISTS context_version;
