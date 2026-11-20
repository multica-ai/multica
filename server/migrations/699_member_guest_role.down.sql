-- Guests and guest invitations must be removed (or converted to members)
-- before rolling back; the restored constraints reject them on the next write.
ALTER TABLE workspace_invitation DROP COLUMN IF EXISTS project_ids;

ALTER TABLE workspace_invitation
    DROP CONSTRAINT IF EXISTS workspace_invitation_role_check,
    ADD CONSTRAINT workspace_invitation_role_check
    CHECK (role IN ('admin', 'member')) NOT VALID;

ALTER TABLE member
    DROP CONSTRAINT IF EXISTS member_role_check,
    ADD CONSTRAINT member_role_check
    CHECK (role IN ('owner', 'admin', 'member')) NOT VALID;
