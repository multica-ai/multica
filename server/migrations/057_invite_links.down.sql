DROP INDEX IF EXISTS idx_invitation_invitee_user;
DROP INDEX IF EXISTS idx_invitation_invitee_email;
DROP INDEX IF EXISTS idx_invitation_link_workspace;
DROP INDEX IF EXISTS idx_invitation_token_hash;
DROP INDEX IF EXISTS idx_invitation_unique_pending_email;

DELETE FROM workspace_invitation WHERE invite_type = 'link';

ALTER TABLE workspace_invitation
    DROP CONSTRAINT IF EXISTS workspace_invitation_usage_check,
    DROP CONSTRAINT IF EXISTS workspace_invitation_email_type_check,
    DROP COLUMN IF EXISTS created_by_user_agent,
    DROP COLUMN IF EXISTS created_by_ip,
    DROP COLUMN IF EXISTS last_used_at,
    DROP COLUMN IF EXISTS revoked_at,
    DROP COLUMN IF EXISTS used_count,
    DROP COLUMN IF EXISTS max_uses,
    DROP COLUMN IF EXISTS token_hash,
    DROP COLUMN IF EXISTS invite_type,
    ALTER COLUMN invitee_email SET NOT NULL;

CREATE UNIQUE INDEX idx_invitation_unique_pending
    ON workspace_invitation(workspace_id, invitee_email) WHERE status = 'pending';

CREATE INDEX idx_invitation_invitee_email ON workspace_invitation(invitee_email) WHERE status = 'pending';
CREATE INDEX idx_invitation_invitee_user  ON workspace_invitation(invitee_user_id) WHERE status = 'pending';
