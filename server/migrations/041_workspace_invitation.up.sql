CREATE TABLE workspace_invitation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    inviter_id UUID NOT NULL REFERENCES "user"(id),
    invitee_email TEXT NOT NULL,
    invitee_user_id UUID REFERENCES "user"(id),
    role TEXT NOT NULL CHECK (role IN ('admin', 'member')),
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'declined', 'expired')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT now() + INTERVAL '7 days'
);

-- Only one pending invitation per workspace + email at a time.
CREATE UNIQUE INDEX idx_invitation_unique_pending
    ON workspace_invitation(workspace_id, invitee_email) WHERE status = 'pending';

-- Fast lookup of pending invitations for a user (by email or user_id).
CREATE INDEX idx_invitation_invitee_email ON workspace_invitation(invitee_email) WHERE status = 'pending';
CREATE INDEX idx_invitation_invitee_user  ON workspace_invitation(invitee_user_id) WHERE status = 'pending';
