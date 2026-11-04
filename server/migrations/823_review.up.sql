-- Reviews (spec 4.6): a reviewer agent's judgement of one subject for one month
-- or week, citing the scorecard it relies on. recommendations is a list of
-- {type, rationale, proposal_id}; each change it recommends is an org_change
-- proposal. The narrative is capped in the database as well as in the API.
-- No foreign keys: the scorecard and reviewer are checked by the publisher.
CREATE TABLE IF NOT EXISTS review (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('agent', 'squad', 'company')),
    subject_id UUID NOT NULL,
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    scorecard_id UUID NOT NULL,
    rating TEXT NOT NULL CHECK (rating IN ('exceeds', 'meets', 'below', 'at_risk')),
    narrative TEXT NOT NULL CHECK (char_length(narrative) <= 8000),
    recommendations JSONB NOT NULL DEFAULT '[]',
    reviewer_agent_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_start < period_end)
);
