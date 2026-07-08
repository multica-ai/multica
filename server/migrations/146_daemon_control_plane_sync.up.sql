ALTER TABLE agent
    ADD COLUMN profile_version INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN runtime_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN memory_policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN approval_policy JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE agent_runtime
    ADD COLUMN endpoint_type TEXT NOT NULL DEFAULT 'daemon'
        CHECK (endpoint_type IN ('daemon', 'cloud_service', 'local_device')),
    ADD COLUMN capabilities JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN models JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN tools JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN runtime_features JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE agent_runtime_profile_state (
    runtime_id UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    desired_version INTEGER NOT NULL,
    applied_version INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'synced', 'failed')),
    error TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (runtime_id, agent_id)
);

INSERT INTO agent_runtime_profile_state (
    runtime_id,
    agent_id,
    desired_version,
    applied_version,
    status
)
SELECT
    runtime_id,
    id,
    profile_version,
    profile_version,
    'synced'
FROM agent
WHERE archived_at IS NULL;

CREATE INDEX idx_agent_runtime_profile_state_pending
    ON agent_runtime_profile_state(runtime_id, status, desired_version, applied_version)
    WHERE status <> 'synced' OR desired_version > applied_version;
