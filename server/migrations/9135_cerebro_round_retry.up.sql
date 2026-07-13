ALTER TABLE cerebro_round_held_trigger
    ADD COLUMN IF NOT EXISTS retry_count INTEGER NOT NULL DEFAULT 0 CHECK (retry_count BETWEEN 0 AND 3);

ALTER TABLE cerebro_round_held_trigger
    DROP CONSTRAINT IF EXISTS cerebro_round_held_trigger_state_check;
ALTER TABLE cerebro_round_held_trigger
    ADD CONSTRAINT cerebro_round_held_trigger_state_check
    CHECK (state IN ('held', 'released', 'cancelled', 'retry', 'failed'));

ALTER TABLE cerebro_round_run_item
    ADD COLUMN IF NOT EXISTS trigger_id UUID REFERENCES cerebro_round_held_trigger(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS cerebro_round_run_item_trigger_idx
    ON cerebro_round_run_item(trigger_id) WHERE trigger_id IS NOT NULL;
