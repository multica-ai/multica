-- What a standing approval records on the proposal it decides (spec 4.4).
-- decided_by_policy_id is set when a policy approved the proposal, as the
-- system actor policy:<id>; decided_by stays empty then. reversed_at marks an
-- auto-approval that a review later reversed (see service ListReversedAutoApprovals).
-- estimated_cost_micros is the filing's cost estimate, empty when the filing gave none.
--
-- All columns are nullable or constant-defaulted, so none rewrites the table.
ALTER TABLE proposal ADD COLUMN IF NOT EXISTS decided_by_policy_id UUID;
ALTER TABLE proposal ADD COLUMN IF NOT EXISTS estimated_cost_micros BIGINT;
ALTER TABLE proposal ADD COLUMN IF NOT EXISTS reversed_at TIMESTAMPTZ;
ALTER TABLE proposal ADD COLUMN IF NOT EXISTS reversal_note TEXT NOT NULL DEFAULT '';
ALTER TABLE proposal DROP CONSTRAINT IF EXISTS proposal_estimated_cost_micros_check;
ALTER TABLE proposal ADD CONSTRAINT proposal_estimated_cost_micros_check
    CHECK (estimated_cost_micros IS NULL OR estimated_cost_micros >= 0) NOT VALID;
ALTER TABLE proposal VALIDATE CONSTRAINT proposal_estimated_cost_micros_check;
