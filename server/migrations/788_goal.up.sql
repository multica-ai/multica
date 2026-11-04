-- Goals (spec 4.1). A goal belongs to a scope (company, floor, squad or
-- initiative), has an owner and a period, and is made of key results (792).
--
-- confidence is the derived value as last computed; confidence_override is the
-- owner's choice, shown as an override with confidence_note. The effective
-- confidence is the override when one is set. confidence_changed_at moves when
-- the derived value changes, which is what the brief reports as a state change.
-- source_proposal_id is the goal proposal that activated a squad's goal.
--
-- No foreign keys. The primary key is built concurrently in 789, attached in 790.
CREATE TABLE IF NOT EXISTS goal (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('company', 'floor', 'squad', 'initiative')),
    scope_id UUID,
    parent_goal_id UUID,
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    why TEXT NOT NULL DEFAULT '' CHECK (char_length(why) <= 4000),
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    owner_type TEXT NOT NULL CHECK (owner_type IN ('squad', 'member', 'agent')),
    owner_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'active', 'achieved', 'missed', 'dropped')),
    confidence TEXT NOT NULL DEFAULT 'unknown'
        CHECK (confidence IN ('on_track', 'at_risk', 'off_track', 'unknown')),
    confidence_override TEXT
        CHECK (confidence_override IN ('on_track', 'at_risk', 'off_track')),
    confidence_note TEXT NOT NULL DEFAULT '' CHECK (char_length(confidence_note) <= 2000),
    confidence_changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    source_proposal_id UUID,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent', 'system')),
    created_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_start < period_end),
    CHECK ((scope_type = 'company') = (scope_id IS NULL)),
    CHECK (confidence_override IS NULL OR char_length(confidence_note) > 0)
);
