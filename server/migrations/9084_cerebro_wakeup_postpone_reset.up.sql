-- TECH-3734: consecutive_postpones changed meaning. It used to increment on
-- every successful dispatch (old IncrementWakeupPostpones); it now counts
-- consecutive offline/no-runtime postpones and resets to 0 on a successful
-- dispatch. Zero the column once at deploy so no live wakeup carries a stale
-- dispatch-era count that would suppress the first wakeup_parked activity
-- (the count == 1 transition gate) or mis-fire the loop notification on the
-- first post-deploy offline streak.
UPDATE cerebro_agent_wakeup
SET consecutive_postpones = 0
WHERE consecutive_postpones <> 0;
