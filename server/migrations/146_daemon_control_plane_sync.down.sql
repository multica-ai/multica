DROP INDEX IF EXISTS idx_agent_runtime_profile_state_pending;
DROP TABLE IF EXISTS agent_runtime_profile_state;

ALTER TABLE agent_runtime
    DROP COLUMN IF EXISTS runtime_features,
    DROP COLUMN IF EXISTS tools,
    DROP COLUMN IF EXISTS models,
    DROP COLUMN IF EXISTS capabilities,
    DROP COLUMN IF EXISTS endpoint_type;

ALTER TABLE agent
    DROP COLUMN IF EXISTS approval_policy,
    DROP COLUMN IF EXISTS memory_policy,
    DROP COLUMN IF EXISTS runtime_policy,
    DROP COLUMN IF EXISTS profile_version;
