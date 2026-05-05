-- CEREBRO-PATCH(migration-idempotent-026-comment-reactions): cerebro modification of upstream file
CREATE TABLE IF NOT EXISTS comment_reaction (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    comment_id UUID NOT NULL REFERENCES comment(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent')),
    actor_id UUID NOT NULL,
    emoji TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (comment_id, actor_type, actor_id, emoji)
);

CREATE INDEX IF NOT EXISTS idx_comment_reaction_comment_id ON comment_reaction(comment_id);
