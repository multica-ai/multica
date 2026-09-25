-- Product-side durable intents for Cloud's pre-purchased seat protocol. This
-- table contains no commercial limits; it only records cross-service writes
-- until their idempotent Cloud operation is settled.
CREATE TABLE seat_capacity_outbox (
    workspace_id UUID NOT NULL,
    operation_token UUID NOT NULL,
    action TEXT NOT NULL CHECK (action IN (
        'reserve_invitation',
        'consume_invitation',
        'claim_share_join',
        'confirm',
        'release',
        'release_member'
    )),
    subject_id UUID,
    member_id UUID,
    invitation_id UUID,
    share_link_id UUID,
    user_id UUID,
    expires_at TIMESTAMPTZ,
    delivered_at TIMESTAMPTZ,
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_token UUID,
    last_error TEXT,
    dead_lettered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((action <> 'reserve_invitation') OR (invitation_id IS NOT NULL AND expires_at IS NOT NULL)),
    CHECK ((action <> 'consume_invitation') OR (invitation_id IS NOT NULL AND user_id IS NOT NULL)),
    CHECK ((action <> 'claim_share_join') OR (share_link_id IS NOT NULL AND user_id IS NOT NULL)),
    CHECK ((action <> 'confirm') OR member_id IS NOT NULL),
    CHECK ((action <> 'release_member') OR member_id IS NOT NULL)
);
