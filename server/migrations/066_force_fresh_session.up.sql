-- Per-task signal that the manual rerun flow uses to short-circuit the
-- (agent_id, issue_id) session resume lookup. Set when a user clicks
-- rerun: the user just judged the prior output bad, so the daemon must
-- start a fresh agent session instead of resuming the same conversation
-- that produced the bad result. Auto-retry of an orphaned mid-flight
-- failure leaves this FALSE so MUL-1128's resume contract is preserved.
ALTER TABLE agent_task_queue
  ADD COLUMN force_fresh_session BOOLEAN NOT NULL DEFAULT FALSE;
