-- Reverting drops the immutable creator. Dispatch then falls back to the
-- pre-MUL-6951 resolution, which reads published_by.
ALTER TABLE autopilot_trigger DROP COLUMN IF EXISTS created_by_id;
ALTER TABLE autopilot_trigger DROP COLUMN IF EXISTS created_by_type;
