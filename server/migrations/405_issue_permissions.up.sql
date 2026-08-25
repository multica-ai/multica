-- 2026-08-24 coder(lq): Store additive, issue-scoped grants separately from
-- project membership. Binding each row to project_id makes a task transfer
-- invalidate grants created under its previous project.
CREATE TABLE issue_permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    permission TEXT NOT NULL CHECK (permission IN ('project.edit', 'project.issue.manage', 'project.agent.use')),
    granted_by UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issue_id, user_id, permission)
);

CREATE INDEX issue_permissions_user_issue_idx ON issue_permissions (user_id, issue_id);
