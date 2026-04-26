-- Reverse 051_shareable_invitation.up.sql. Any existing rows with
-- invitee_email IS NULL (shareable links) would violate the reinstated
-- NOT NULL constraint; delete them first so the migration is reversible
-- on a database that has issued shareable links.
DELETE FROM workspace_invitation WHERE invitee_email IS NULL;

ALTER TABLE workspace_invitation
    DROP COLUMN use_count,
    DROP COLUMN max_uses,
    ALTER COLUMN invitee_email SET NOT NULL;
