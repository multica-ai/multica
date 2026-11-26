-- People removed from a workspace with "block from rejoining" (MUL-10296).
-- Matched by email, which never changes for an account and is what an
-- invitation names before an account exists. No foreign keys: workspace
-- deletion removes the rows.
CREATE TABLE IF NOT EXISTS workspace_member_block (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    email TEXT NOT NULL CHECK (email = lower(email)),
    blocked_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
