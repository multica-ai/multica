-- Reverse 9045_cerebro_cost_optimization_holdout.
ALTER TABLE cerebro_cost_optimization_measurement
    DROP COLUMN IF EXISTS held_out,
    DROP COLUMN IF EXISTS actual_cost_cents;
