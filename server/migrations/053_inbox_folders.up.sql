-- CEREBRO-PATCH(migration-idempotent-053-inbox-folders): cerebro modification of upstream file
-- Per-user inbox folders (labels) for organizing chat sessions and notifications.
-- Replaces chat-session pinning in the inbox UI. Items with at least one folder
-- membership disappear from the default inbox view and appear in their folder(s).

CREATE TABLE IF NOT EXISTS inbox_folder (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    position FLOAT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_inbox_folder_user_ws ON inbox_folder(workspace_id, user_id, position);

CREATE TABLE IF NOT EXISTS inbox_folder_membership (
    folder_id UUID NOT NULL REFERENCES inbox_folder(id) ON DELETE CASCADE,
    item_type TEXT NOT NULL CHECK (item_type IN ('chat_session', 'notification')),
    item_id UUID NOT NULL,
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (folder_id, item_type, item_id)
);

CREATE INDEX IF NOT EXISTS idx_inbox_folder_membership_item ON inbox_folder_membership(item_type, item_id);

-- Chat-session pinning is replaced by folders. Drop any existing chat_session pins.
DELETE FROM pinned_item WHERE item_type = 'chat_session';
