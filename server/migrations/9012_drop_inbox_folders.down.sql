-- Recreate inbox folder tables in their last (post-054) shape.
-- Membership data is not recoverable.

CREATE TABLE IF NOT EXISTS inbox_folder (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    position FLOAT NOT NULL DEFAULT 0,
    parent_id UUID REFERENCES inbox_folder(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_inbox_folder_user_ws ON inbox_folder(workspace_id, user_id, position);
CREATE INDEX IF NOT EXISTS idx_inbox_folder_parent ON inbox_folder(parent_id);

CREATE TABLE IF NOT EXISTS inbox_folder_membership (
    folder_id UUID NOT NULL REFERENCES inbox_folder(id) ON DELETE CASCADE,
    item_type TEXT NOT NULL CHECK (item_type IN ('chat_session', 'notification')),
    item_id UUID NOT NULL,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (folder_id, item_type, item_id)
);

CREATE INDEX IF NOT EXISTS idx_inbox_folder_membership_item ON inbox_folder_membership(item_type, item_id);
