ALTER TABLE cerebro_eval_schedule
    ADD COLUMN IF NOT EXISTS claimed_until TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS cerebro_eval_schedule_claim_due_idx
    ON cerebro_eval_schedule (next_run_at, claimed_until)
    WHERE enabled;
