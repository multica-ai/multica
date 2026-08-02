-- WS-750: per-autopilot concurrent run cap.
--
-- When set, DispatchAutopilot counts this autopilot's in-flight runs
-- (status issue_created/running) and records a `skipped` run instead of
-- stacking another dispatch that would exceed the cap. NULL = unlimited,
-- preserving the pre-existing behaviour. The skip reuses the m079 `skipped`
-- terminal status and the existing recordSkippedRun path, so - unlike the
-- concurrency_policy column dropped in 043 (skip orphan bug / queue never
-- queued / replace never cancelled) - no orphaned run is left behind.
--
-- CHECK >= 1: 0 would skip every dispatch (use status='paused' for that).
ALTER TABLE autopilot ADD COLUMN IF NOT EXISTS max_concurrent_runs INTEGER
    CHECK (max_concurrent_runs IS NULL OR max_concurrent_runs >= 1);
