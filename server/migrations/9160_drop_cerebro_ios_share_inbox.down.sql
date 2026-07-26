CREATE TABLE IF NOT EXISTS cerebro_ios_share_inbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id uuid NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    owner_user_id uuid NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    name text NOT NULL DEFAULT 'iPhone Share Sheet',
    token_hash text NOT NULL UNIQUE,
    token_prefix text NOT NULL,
    last_used_at timestamptz,
    revoked_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS cerebro_ios_share_inbox_owner_idx
    ON cerebro_ios_share_inbox (workspace_id, owner_user_id, created_at DESC);
