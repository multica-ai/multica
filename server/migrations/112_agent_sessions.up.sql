-- Migration: Create agent_sessions table
-- Supports session persistence for agent runs on specific issues.
-- Ref: S3-F3 (multica#3625)

CREATE TABLE IF NOT EXISTS agent_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    run_number INTEGER NOT NULL DEFAULT 1,
    state JSONB NOT NULL DEFAULT '{}',
    conversation_summary TEXT,
    working_directory TEXT,
    branch TEXT,
    files_modified TEXT[],
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    version INTEGER NOT NULL DEFAULT 1,
    UNIQUE(issue_id, agent_id, run_number)
);

-- Index for fast lookup of active sessions by issue+agent.
CREATE INDEX idx_sessions_active
    ON agent_sessions(issue_id, agent_id)
    WHERE is_active = true;

-- Index for efficient cleanup of expired sessions.
CREATE INDEX idx_sessions_expiry
    ON agent_sessions(expires_at)
    WHERE is_active = true;

-- Index for chronological ordering of runs.
CREATE INDEX idx_sessions_run_number
    ON agent_sessions(issue_id, agent_id, run_number DESC);
