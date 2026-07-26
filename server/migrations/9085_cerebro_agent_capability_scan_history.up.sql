CREATE TABLE IF NOT EXISTS cerebro_agent_capability_scan (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    window_start_at TIMESTAMPTZ NOT NULL,
    window_end_at TIMESTAMPTZ NOT NULL,
    observed_capability_count INTEGER NOT NULL DEFAULT 0,
    new_capability_count INTEGER NOT NULL DEFAULT 0,
    drift_capability_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (window_end_at >= window_start_at)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_agent_capability_scan_agent_completed
    ON cerebro_agent_capability_scan(agent_id, completed_at DESC);

CREATE INDEX IF NOT EXISTS idx_cerebro_agent_capability_scan_workspace_completed
    ON cerebro_agent_capability_scan(workspace_id, completed_at DESC);

CREATE TABLE IF NOT EXISTS cerebro_agent_capability_scan_item (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scan_id UUID NOT NULL REFERENCES cerebro_agent_capability_scan(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    capability_type TEXT NOT NULL CHECK (capability_type IN ('tool')),
    capability_key TEXT NOT NULL,
    display_name TEXT NOT NULL,
    uses BIGINT NOT NULL DEFAULT 0,
    first_seen_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ,
    permission TEXT,
    status TEXT NOT NULL CHECK (status IN ('allowed', 'needs_approval', 'blocked', 'unmapped')),
    is_new BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (scan_id, capability_type, capability_key)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_agent_capability_scan_item_agent_key
    ON cerebro_agent_capability_scan_item(agent_id, capability_type, capability_key);

CREATE INDEX IF NOT EXISTS idx_cerebro_agent_capability_scan_item_new
    ON cerebro_agent_capability_scan_item(workspace_id, agent_id, is_new, created_at DESC)
    WHERE is_new = true;
