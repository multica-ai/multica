-- Collaboration requests are the server-validated alternative to raw
-- mention://agent handoffs for supervised_collaboration agents.
CREATE TABLE collaboration_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    from_agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    to_agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    parent_request_id UUID REFERENCES collaboration_request(id) ON DELETE SET NULL,
    trigger_comment_id UUID REFERENCES comment(id) ON DELETE SET NULL,
    target_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'accepted'
        CHECK (status IN ('accepted', 'queued', 'failed', 'cancelled', 'completed')),
    mode TEXT NOT NULL DEFAULT 'discussion_only'
        CHECK (mode IN ('discussion_only')),
    purpose TEXT NOT NULL CHECK (length(btrim(purpose)) > 0),
    max_turns INT NOT NULL DEFAULT 2 CHECK (max_turns > 0),
    depth INT NOT NULL DEFAULT 1 CHECK (depth > 0),
    expires_at TIMESTAMPTZ NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (from_agent_id <> to_agent_id)
);

CREATE INDEX idx_collaboration_request_issue_created
    ON collaboration_request(issue_id, created_at DESC);
CREATE INDEX idx_collaboration_request_workspace_status
    ON collaboration_request(workspace_id, status, expires_at);
CREATE INDEX idx_collaboration_request_agent_pair_active
    ON collaboration_request(issue_id, from_agent_id, to_agent_id, status, expires_at);
