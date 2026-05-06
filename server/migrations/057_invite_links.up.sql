ALTER TABLE workspace_invitation
    ALTER COLUMN invitee_email DROP NOT NULL,
    ADD COLUMN invite_type TEXT NOT NULL DEFAULT 'email' CHECK (invite_type IN ('email', 'link')),
    ADD COLUMN token_hash TEXT,
    ADD COLUMN max_uses INTEGER NOT NULL DEFAULT 1 CHECK (max_uses > 0),
    ADD COLUMN used_count INTEGER NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    ADD COLUMN revoked_at TIMESTAMPTZ,
    ADD COLUMN last_used_at TIMESTAMPTZ,
    ADD COLUMN created_by_ip TEXT,
    ADD COLUMN created_by_user_agent TEXT,
    ADD CONSTRAINT workspace_invitation_email_type_check
        CHECK (
            (invite_type = 'email' AND invitee_email IS NOT NULL AND token_hash IS NULL)
            OR
            (invite_type = 'link' AND invitee_email IS NULL AND token_hash IS NOT NULL)
        ),
    ADD CONSTRAINT workspace_invitation_usage_check
        CHECK (used_count <= max_uses);

DROP INDEX IF EXISTS idx_invitation_unique_pending;
DROP INDEX IF EXISTS idx_invitation_invitee_email;
DROP INDEX IF EXISTS idx_invitation_invitee_user;

CREATE UNIQUE INDEX idx_invitation_unique_pending_email
    ON workspace_invitation(workspace_id, invitee_email)
    WHERE status = 'pending' AND invite_type = 'email';

CREATE UNIQUE INDEX idx_invitation_token_hash
    ON workspace_invitation(token_hash)
    WHERE invite_type = 'link';

CREATE INDEX idx_invitation_link_workspace
    ON workspace_invitation(workspace_id, created_at DESC)
    WHERE invite_type = 'link';

CREATE INDEX idx_invitation_invitee_email
    ON workspace_invitation(invitee_email)
    WHERE status = 'pending' AND invite_type = 'email';

CREATE INDEX idx_invitation_invitee_user
    ON workspace_invitation(invitee_user_id)
    WHERE status = 'pending' AND invite_type = 'email';
