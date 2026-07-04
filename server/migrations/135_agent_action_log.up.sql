-- Phase 3 control plane part 1: agent action audit log.
-- Records every tool call an agent makes so operational agents that touch
-- business systems are fully traceable. Additive only, no behavior change
-- until the daemon writes rows here.
CREATE TABLE IF NOT EXISTS agent_action_log (
    id             BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    agent_id       TEXT,
    issue_id       TEXT,
    tool_name      TEXT NOT NULL,
    args_summary   TEXT,
    result_summary TEXT,
    status         TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_agent_action_log_agent_created
    ON agent_action_log (agent_id, created_at DESC);
