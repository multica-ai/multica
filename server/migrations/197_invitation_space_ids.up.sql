-- Space targeting for invitations: which spaces the invitee joins on accept.
-- Empty array = fall back to the workspace default space, which keeps older
-- clients (that send no space_ids) on their existing behaviour.
ALTER TABLE workspace_invitation
    ADD COLUMN space_ids UUID[] NOT NULL DEFAULT '{}';
