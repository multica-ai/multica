CREATE TABLE multica_agent_audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL,
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    status_code INTEGER DEFAULT 0,
    error_msg TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_agent_audit_agent_id ON multica_agent_audit_logs (agent_id);
CREATE INDEX idx_agent_audit_created_at ON multica_agent_audit_logs (created_at DESC);
