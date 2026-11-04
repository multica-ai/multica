ALTER TABLE proposal DROP CONSTRAINT IF EXISTS proposal_estimated_cost_micros_check;
ALTER TABLE proposal DROP COLUMN IF EXISTS reversal_note;
ALTER TABLE proposal DROP COLUMN IF EXISTS reversed_at;
ALTER TABLE proposal DROP COLUMN IF EXISTS estimated_cost_micros;
ALTER TABLE proposal DROP COLUMN IF EXISTS decided_by_policy_id;
