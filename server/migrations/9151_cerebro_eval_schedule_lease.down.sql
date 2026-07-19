DROP INDEX IF EXISTS cerebro_eval_schedule_claim_due_idx;
ALTER TABLE cerebro_eval_schedule DROP COLUMN IF EXISTS claimed_until;
