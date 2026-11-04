-- One row per applied org_change proposal (spec 4.6). before_state is what the
-- change replaced, which is what a revert restores. The probation fields hold
-- the 14-day window, the two scorecards compared and the outcome: improved,
-- neutral or worse. A row is written in the transaction that applies the change.
-- No foreign keys.
CREATE TABLE IF NOT EXISTS org_change_application (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    proposal_id UUID NOT NULL,
    change_type TEXT NOT NULL CHECK (change_type IN ('instruction_change', 'skill_change',
        'charter_change', 'autonomy_change', 'hire', 'retire', 'move', 'merge_squads')),
    subject_type TEXT NOT NULL CHECK (subject_type IN ('agent', 'squad')),
    subject_id UUID NOT NULL,
    before_state JSONB NOT NULL DEFAULT '{}',
    after_state JSONB NOT NULL DEFAULT '{}',
    applied_by_type TEXT NOT NULL CHECK (applied_by_type IN ('member', 'agent', 'system', 'policy')),
    applied_by_id UUID,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reverted_at TIMESTAMPTZ,
    reverted_by_type TEXT,
    reverted_by_id UUID,
    probation_ends_at TIMESTAMPTZ,
    before_scorecard_id UUID,
    after_scorecard_id UUID,
    probation_outcome TEXT CHECK (probation_outcome IN ('improved', 'neutral', 'worse')),
    probation_note TEXT NOT NULL DEFAULT '' CHECK (char_length(probation_note) <= 2000),
    probation_checked_at TIMESTAMPTZ,
    rollback_proposal_id UUID
);
