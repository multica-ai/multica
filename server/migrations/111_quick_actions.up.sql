-- Workspace-shared quick actions: reusable comment macros surfaced as buttons
-- on the issue detail sidebar. Clicking one posts its body as a comment on the
-- issue, which can kick off agent work. Shared across the whole workspace
-- (not per-user), so every member sees the same set.
CREATE TABLE quick_action (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    label TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX quick_action_workspace_idx ON quick_action (workspace_id);
