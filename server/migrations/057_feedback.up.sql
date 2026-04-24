CREATE TABLE feedback (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    workspace_id  UUID REFERENCES workspace(id) ON DELETE SET NULL,
    message       TEXT NOT NULL,
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_feedback_user_created ON feedback(user_id, created_at DESC);
