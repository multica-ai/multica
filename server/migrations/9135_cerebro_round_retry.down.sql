DROP INDEX IF EXISTS cerebro_round_run_item_trigger_idx;
ALTER TABLE cerebro_round_run_item DROP COLUMN IF EXISTS trigger_id;

UPDATE cerebro_round_held_trigger
SET state = 'released'
WHERE state IN ('retry', 'failed');

ALTER TABLE cerebro_round_held_trigger
    DROP CONSTRAINT IF EXISTS cerebro_round_held_trigger_state_check;
ALTER TABLE cerebro_round_held_trigger
    ADD CONSTRAINT cerebro_round_held_trigger_state_check
    CHECK (state IN ('held', 'released', 'cancelled'));

ALTER TABLE cerebro_round_held_trigger DROP COLUMN IF EXISTS retry_count;
