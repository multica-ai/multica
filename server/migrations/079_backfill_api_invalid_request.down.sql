-- Reverse the backfill: reset the rows we re-classified back to the
-- legacy 'agent_error' default. The error text we use as the witness is
-- preserved on the row so the same WHERE clause still selects the same
-- set unless someone manually relabels in between.
UPDATE agent_task_queue
SET failure_reason = 'agent_error'
WHERE status = 'failed'
  AND failure_reason = 'api_invalid_request'
  AND error ILIKE '%400%'
  AND error ILIKE '%invalid_request_error%';
