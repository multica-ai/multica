CREATE TABLE IF NOT EXISTS cerebro_feature_flags (
    workspace_id uuid NOT NULL,
    user_id uuid NOT NULL,
    flag_key text NOT NULL,
    enabled boolean NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workspace_id, user_id, flag_key)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_feature_flags_user
    ON cerebro_feature_flags (workspace_id, user_id);
