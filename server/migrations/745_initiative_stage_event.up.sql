-- The initiative timeline (spec 3.1): one row per transition, append-only. Rows
-- leave only with the workspace. No foreign keys; the primary key is built in
-- 746 and attached in 747, and the timeline index is 748.
CREATE TABLE IF NOT EXISTS initiative_stage_event (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    initiative_id UUID NOT NULL,
    from_stage TEXT,
    to_stage TEXT,
    kind TEXT NOT NULL CHECK (kind IN ('created', 'advanced', 'sent_back', 'shelved',
        'resumed', 'closed', 'forced', 'attested', 'artifact_waived')),
    actor_type TEXT NOT NULL CHECK (actor_type IN ('agent', 'member', 'system')),
    actor_id UUID,
    gate_proposal_id UUID,
    note TEXT NOT NULL DEFAULT '' CHECK (char_length(note) <= 4000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
