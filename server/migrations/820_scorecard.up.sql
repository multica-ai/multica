-- Scorecards (spec 4.6): one row per subject, period and period start, written
-- by the nightly scorecard job. subject_id is the agent or squad id; for the
-- company it is the workspace id, so one unique key covers every subject.
-- metrics holds each metric's value and its inputs; sample_size is the number of
-- scored runs, and status is insufficient when it is too small to review.
-- No foreign keys: the subject is validated by the job that writes the row.
CREATE TABLE IF NOT EXISTS scorecard (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('agent', 'squad', 'company')),
    subject_id UUID NOT NULL,
    period TEXT NOT NULL CHECK (period IN ('week', 'month')),
    period_start DATE NOT NULL,
    period_end DATE NOT NULL,
    metrics JSONB NOT NULL DEFAULT '{}',
    sample_size INTEGER NOT NULL DEFAULT 0 CHECK (sample_size >= 0),
    status TEXT NOT NULL CHECK (status IN ('ok', 'insufficient')),
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_start < period_end)
);
