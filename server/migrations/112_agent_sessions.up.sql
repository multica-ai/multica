-- Agent session persistence: allows consecutive runs on the same issue
-- to resume from where the last run left off, maintaining conversation
-- continuity and avoiding duplicate analysis.

CREATE TABLE agent_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    run_number INTEGER NOT NULL DEFAULT 1,
    state JSONB NOT NULL DEFAULT '{}',
    conversation_summary TEXT,
    working_directory TEXT,
    branch TEXT,
    files_modified TEXT[] DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    version INTEGER NOT NULL DEFAULT 1,
    UNIQUE(issue_id, agent_id, run_number)
);

-- Fast lookup of active sessions for a given issue+agent pair.
CREATE INDEX idx_sessions_active
    ON agent_sessions(issue_id, agent_id)
    WHERE is_active = true;

-- Efficient cleanup of expired sessions.
CREATE INDEX idx_sessions_expiry
    ON agent_sessions(expires_at)
    WHERE is_active = true;

-- JSONB state queries (e.g., containment or key existence).
CREATE INDEX idx_sessions_state_gin
    ON agent_sessions USING gin(state);

-- Auto-update last_active_at on any row modification.
CREATE OR REPLACE FUNCTION update_agent_sessions_last_active_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.last_active_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_agent_sessions_last_active_at
    BEFORE UPDATE ON agent_sessions
    FOR EACH ROW
    EXECUTE FUNCTION update_agent_sessions_last_active_at();

COMMENT ON TABLE agent_sessions IS 'Tracks agent run sessions for issue-level session persistence and resume';
COMMENT ON COLUMN agent_sessions.state IS 'JSONB blob storing session state: messages, tool results, context';
COMMENT ON COLUMN agent_sessions.conversation_summary IS 'Compressed summary of prior conversation for context injection';
COMMENT ON COLUMN agent_sessions.run_number IS 'Monotonically increasing per (issue_id, agent_id) pair';
COMMENT ON COLUMN agent_sessions.version IS 'Optimistic locking version for concurrent state updates';
