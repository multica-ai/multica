CREATE TABLE mention_frequency (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent')),
    actor_id UUID NOT NULL,
    mentioned_by UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    frequency BIGINT NOT NULL DEFAULT 0,
    last_mentioned_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, actor_type, actor_id, mentioned_by)
);

CREATE INDEX mention_frequency_lookup_idx
ON mention_frequency (workspace_id, mentioned_by, last_mentioned_at DESC);
