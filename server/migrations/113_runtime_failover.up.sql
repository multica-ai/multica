-- Runtime failover: priority + failover groups
-- Adds priority ordering and failover group membership to agent_runtime,
-- enabling automatic task re-routing when a runtime goes offline.

CREATE TABLE runtime_failover_group (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    strategy    TEXT NOT NULL DEFAULT 'priority',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT runtime_failover_group_strategy_check
        CHECK (strategy IN ('priority', 'round-robin', 'least-loaded'))
);

CREATE INDEX idx_runtime_failover_group_workspace
    ON runtime_failover_group (workspace_id);

ALTER TABLE agent_runtime
    ADD COLUMN priority INT NOT NULL DEFAULT 0;

ALTER TABLE agent_runtime
    ADD COLUMN failover_group_id UUID REFERENCES runtime_failover_group(id) ON DELETE SET NULL;

CREATE INDEX idx_agent_runtime_failover_group
    ON agent_runtime (failover_group_id) WHERE failover_group_id IS NOT NULL;
