CREATE TABLE IF NOT EXISTS model_tier_map (
    workspace_id UUID REFERENCES workspace(id) ON DELETE CASCADE,
    tier TEXT NOT NULL,
    concrete TEXT NOT NULL,
    UNIQUE NULLS NOT DISTINCT (workspace_id, tier)
);

INSERT INTO model_tier_map (workspace_id, tier, concrete) VALUES
    (NULL, 'cheap', 'mimo'),
    (NULL, 'balanced', 'muse-spark'),
    (NULL, 'premium', 'qwen')
ON CONFLICT (workspace_id, tier) DO NOTHING;
