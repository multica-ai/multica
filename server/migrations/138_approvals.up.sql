CREATE TABLE approvals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    requester_type TEXT NOT NULL CHECK (requester_type IN ('member', 'agent')),
    requester_id UUID NOT NULL,
    approver_type TEXT NOT NULL CHECK (approver_type IN ('member', 'agent')),
    approver_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    comment TEXT,
    decided_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_approvals_issue ON approvals(issue_id);
CREATE INDEX idx_approvals_approver ON approvals(approver_type, approver_id, status);
CREATE INDEX idx_approvals_workspace ON approvals(workspace_id);
