CREATE TABLE IF NOT EXISTS cerebro_agent_wakeup (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id uuid NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    issue_id uuid NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    prompt text NOT NULL,
    trigger_type text NOT NULL CHECK (trigger_type IN ('time', 'issue_status', 'github_ci')),
    fire_at timestamptz,
    watch_issue_id uuid REFERENCES issue(id) ON DELETE CASCADE,
    watch_status text,
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'claimed', 'dispatched', 'cancelled', 'failed')),
    claimed_at timestamptz,
    dispatched_at timestamptz,
    cancelled_at timestamptz,
    failure text,
    created_by_id uuid REFERENCES "user"(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CHECK (
        (trigger_type = 'time' AND fire_at IS NOT NULL AND watch_issue_id IS NULL AND watch_status IS NULL)
        OR (trigger_type = 'issue_status' AND fire_at IS NULL AND watch_issue_id IS NOT NULL AND watch_status IS NOT NULL)
        OR (trigger_type = 'github_ci' AND fire_at IS NULL AND watch_issue_id IS NOT NULL AND watch_status IS NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_cerebro_agent_wakeup_due_time
    ON cerebro_agent_wakeup (fire_at)
    WHERE state = 'pending' AND trigger_type = 'time';

CREATE INDEX IF NOT EXISTS idx_cerebro_agent_wakeup_issue_status
    ON cerebro_agent_wakeup (watch_issue_id, watch_status)
    WHERE state = 'pending' AND trigger_type = 'issue_status';

CREATE INDEX IF NOT EXISTS idx_cerebro_agent_wakeup_github_ci
    ON cerebro_agent_wakeup (watch_issue_id)
    WHERE state = 'pending' AND trigger_type = 'github_ci';

CREATE INDEX IF NOT EXISTS idx_cerebro_agent_wakeup_agent_workspace
    ON cerebro_agent_wakeup (workspace_id, agent_id, created_at DESC);
