-- Pinned items: per-user quick-access items in the sidebar
CREATE TABLE pinned_item (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    item_type TEXT NOT NULL CHECK (item_type IN ('issue', 'project')),
    item_id UUID NOT NULL,
    position FLOAT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, user_id, item_type, item_id)
);

CREATE INDEX idx_pinned_item_user_ws ON pinned_item (workspace_id, user_id, position);
