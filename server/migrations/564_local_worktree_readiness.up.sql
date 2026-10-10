CREATE TABLE IF NOT EXISTS local_worktree_readiness (
 resource_id uuid NOT NULL,
 resource_ref jsonb NOT NULL,
 checked_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 status text NOT NULL,
 measurement jsonb NOT NULL DEFAULT '{}'
);
