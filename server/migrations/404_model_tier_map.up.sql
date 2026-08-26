CREATE TABLE model_tier_map (
    workspace_id UUID NULL REFERENCES workspace(id) ON DELETE CASCADE,
    tier TEXT NOT NULL,
    concrete TEXT NOT NULL,
    PRIMARY KEY(workspace_id, tier)
);

INSERT INTO model_tier_map (workspace_id, tier, concrete) VALUES
    (NULL, 'cheap', 'mimo'),
    (NULL, 'balanced', 'muse-spark'),
    (NULL, 'premium', 'qwen');
