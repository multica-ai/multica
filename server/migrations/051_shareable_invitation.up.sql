-- Shareable invitation links.
--
-- Existing targeted invitations keep invitee_email populated and behave as
-- before. Shareable links leave invitee_email NULL; anyone with the
-- invitation URL who is authenticated can redeem until max_uses is hit or
-- the link is revoked.
ALTER TABLE workspace_invitation
    ALTER COLUMN invitee_email DROP NOT NULL,
    ADD COLUMN max_uses  INT,
    ADD COLUMN use_count INT NOT NULL DEFAULT 0;

-- The existing `idx_invitation_unique_pending (workspace_id, invitee_email)`
-- partial unique index continues to enforce at-most-one pending targeted
-- invitation per (workspace, email). Postgres treats NULL values as distinct
-- for UNIQUE constraints, so multiple shareable links with NULL email can
-- coexist in the same workspace.
