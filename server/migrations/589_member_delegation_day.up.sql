-- One row per (member, UTC day) on which that member personally started an
-- agent run: commented, mentioned, assigned, promoted, created an assigned
-- issue, triggered, reran, chatted or quick-created. Written at the moment of
-- the human action, so it keeps every human who acted and the day they acted
-- even when a later comment folds into a queued run and re-attributes it, and
-- it never records runs that only inherit a human (retries, system wakeups,
-- delegation, autopilot). Not written when telemetry is off (DO_NOT_TRACK).
--
-- Feeds the self-host telemetry "delegating member" counts; no other reader.
-- No FK to "user" by design (workspace DB rules); readers join current
-- members. The primary key is built concurrently in 590/591, following
-- client_usage_daily (207-209), so no migration builds an index inline.
CREATE TABLE member_delegation_day (
    user_id       UUID NOT NULL,
    activity_date DATE NOT NULL
);
