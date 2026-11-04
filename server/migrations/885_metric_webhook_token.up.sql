-- Workspace-scoped tokens for outside metric feeds (spec 4.1). A token belongs to
-- one workspace and reports readings to it through the webhook route. It is not a
-- member's credential, so a member leaving the workspace does not change it.
-- Only the SHA-256 of the token is stored. revoked_at is the delete path: a
-- revoked token is kept as history and no longer authenticates.
--
-- No primary key and no foreign keys (project rule): the id index is built
-- concurrently in 886.
CREATE TABLE IF NOT EXISTS metric_webhook_token (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    token_hash TEXT NOT NULL,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);
