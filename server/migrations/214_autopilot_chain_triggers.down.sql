-- Reverse migration 214: drop chain trigger columns and restore the original
-- kind / source CHECKs. The partial unique index from migration 215 is dropped
-- by its own down migration before this runs (lower number down runs first).

ALTER TABLE autopilot_run
    DROP COLUMN IF EXISTS chain_upstream_run_id,
    DROP COLUMN IF EXISTS chain_depth;

ALTER TABLE autopilot_run DROP CONSTRAINT IF EXISTS autopilot_run_source_check;
ALTER TABLE autopilot_run ADD CONSTRAINT autopilot_run_source_check
    CHECK (source IN ('schedule', 'manual', 'webhook', 'api'));

ALTER TABLE autopilot_trigger DROP CONSTRAINT IF EXISTS autopilot_trigger_chain_upstream_check;

ALTER TABLE autopilot_trigger
    DROP COLUMN IF EXISTS upstream_autopilot_id,
    DROP COLUMN IF EXISTS chain_on_status;

ALTER TABLE autopilot_trigger DROP CONSTRAINT IF EXISTS autopilot_trigger_kind_check;
ALTER TABLE autopilot_trigger ADD CONSTRAINT autopilot_trigger_kind_check
    CHECK (kind IN ('schedule', 'webhook', 'api'));
