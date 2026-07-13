-- Phase 2 foundation: Space discoverability and collaboration roles.
ALTER TABLE workspace_space
    ADD COLUMN visibility TEXT NOT NULL DEFAULT 'open'
        CHECK (visibility IN ('open', 'private'));

ALTER TABLE workspace_space
    ADD CONSTRAINT workspace_space_default_must_be_open
        CHECK (NOT is_default OR visibility = 'open');

ALTER TABLE workspace_space_member
    DROP CONSTRAINT workspace_space_member_role_check;

ALTER TABLE workspace_space_member
    ADD CONSTRAINT workspace_space_member_role_check
        CHECK (role IN ('lead', 'admin', 'member', 'guest'));

CREATE INDEX idx_workspace_space_visibility
    ON workspace_space(workspace_id, visibility)
    WHERE archived_at IS NULL;
