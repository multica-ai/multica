-- CEREBRO-PATCH(cerebro-focus-list): FIR-2947
-- Personal focus list: a lightweight to-do surface pinned to the top of the inbox.
-- Each row belongs to one workspace member and may optionally reference an issue.
CREATE TABLE IF NOT EXISTS cerebro_focus_list_item (
    id            UUID             DEFAULT gen_random_uuid() PRIMARY KEY,
    workspace_id  UUID             NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    member_id     UUID             NOT NULL,
    text          TEXT             NOT NULL DEFAULT '',
    issue_id      UUID             REFERENCES issue(id) ON DELETE SET NULL,
    snoozed_until TIMESTAMPTZ,
    done_at       TIMESTAMPTZ,
    position      DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

-- Fast lookup of a member's active (non-done) items in a workspace.
CREATE INDEX IF NOT EXISTS idx_cerebro_focus_list_member
    ON cerebro_focus_list_item(workspace_id, member_id, done_at);
