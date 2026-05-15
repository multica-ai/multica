CREATE TABLE IF NOT EXISTS cerebro_account_token_usage (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id uuid NOT NULL REFERENCES cerebro_account(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    tokens bigint NOT NULL CHECK (tokens >= 0),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_account_token_usage_account_created
    ON cerebro_account_token_usage (account_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_cerebro_account_token_usage_workspace_created
    ON cerebro_account_token_usage (workspace_id, created_at DESC);
