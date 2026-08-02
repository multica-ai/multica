-- WS-750: drop the per-autopilot concurrent run cap.
ALTER TABLE autopilot DROP COLUMN IF EXISTS max_concurrent_runs;
