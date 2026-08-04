-- FIR-4359, second half of the same slow DELETE — see
-- 9172_cerebro_model_usage_event_autopilot_run_index.
--
-- agent_task_queue.autopilot_run_id (042_autopilot) is the other unindexed
-- ON DELETE SET NULL reference to autopilot_run(id). Deleting an autopilot
-- therefore scanned the task queue once per cascaded run as well, on top of
-- the ledger scan.
--
-- Partial for the same reason: only autopilot-dispatched tasks carry a value.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_autopilot_run
    ON agent_task_queue (autopilot_run_id)
    WHERE autopilot_run_id IS NOT NULL;
