-- Shareable invite links for workspaces (complement to email invitations).
--
-- id is NOT declared inline as PRIMARY KEY: the repo convention (see
-- migrations 207-209 / 227-229) is to build the table without a primary key,
-- create the backing unique index CONCURRENTLY in its own single-statement
-- migration (328), then attach it with PRIMARY KEY USING INDEX (329). The
-- workspace_id partial unique index (330) enforces the one-active-link-per-
-- workspace invariant and is created CONCURRENTLY as well.
CREATE TABLE workspace_share_link (
    id           UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    code         TEXT NOT NULL,
    created_by   UUID NOT NULL,
    role         TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    expires_at   TIMESTAMPTZ,
    max_uses     INT,
    use_count    INT NOT NULL DEFAULT 0,
    is_active    BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
