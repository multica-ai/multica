-- MUL-9420: guests. A guest is a workspace member row with role 'guest' that
-- sees only the projects it is a project_member of. An invitation records the
-- projects a guest invitee will join on acceptance.
--
-- Widening a whitelist cannot fail on existing rows, so the constraints are
-- replaced NOT VALID (no scan), the same as 610. The project_ids default is a
-- constant, which keeps the ADD COLUMN catalog-only.
ALTER TABLE member
    DROP CONSTRAINT IF EXISTS member_role_check,
    ADD CONSTRAINT member_role_check
    CHECK (role IN ('owner', 'admin', 'member', 'guest')) NOT VALID;

ALTER TABLE workspace_invitation
    DROP CONSTRAINT IF EXISTS workspace_invitation_role_check,
    ADD CONSTRAINT workspace_invitation_role_check
    CHECK (role IN ('admin', 'member', 'guest')) NOT VALID;

ALTER TABLE workspace_invitation
    ADD COLUMN IF NOT EXISTS project_ids UUID[] NOT NULL DEFAULT '{}';
