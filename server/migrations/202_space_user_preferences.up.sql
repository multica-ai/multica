-- Personal Space navigation and notification preferences are independent from
-- formal Space membership. A Workspace member may pin or follow an accessible
-- Open Space without joining it; neither action grants collaboration access.
CREATE TABLE workspace_space_preference (
    workspace_id UUID NOT NULL,
    space_id UUID NOT NULL,
    user_id UUID NOT NULL,
    is_pinned BOOLEAN NOT NULL DEFAULT false,
    is_followed BOOLEAN NOT NULL DEFAULT false,
    sort_order DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (space_id, user_id),
    FOREIGN KEY (workspace_id, space_id)
        REFERENCES workspace_space(workspace_id, id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id, user_id)
        REFERENCES member(workspace_id, user_id) ON DELETE CASCADE
);

-- Preserve every member's existing personal Sidebar order. Joined Spaces are
-- still shown because of membership; this row only separates future pin,
-- follow, and reorder state from that permission-bearing relationship.
INSERT INTO workspace_space_preference (
    workspace_id, space_id, user_id, sort_order
)
SELECT workspace_id, space_id, user_id, sort_order
FROM workspace_space_member;

CREATE INDEX idx_workspace_space_preference_sidebar
    ON workspace_space_preference(workspace_id, user_id, sort_order)
    WHERE is_pinned;

CREATE INDEX idx_workspace_space_preference_followers
    ON workspace_space_preference(workspace_id, space_id, user_id)
    WHERE is_followed;
