-- Backfill the api_invalid_request poisoned-session classifier introduced
-- in MUL-1921. The daemon now tags failed tasks whose error matches an
-- Anthropic 400 invalid_request_error so GetLastTaskSession excludes them
-- from the (agent_id, issue_id) resume lookup, but that change is
-- forward-only: existing failed rows still carry the default
-- failure_reason='agent_error' and the resume query falls back to them
-- on the next claim, re-poisoning every retry.
--
-- Re-classify any historical row whose error text matches the same
-- canonical shape (case-insensitive substrings "400" and
-- "invalid_request_error") so deploying this PR actually unblocks issues
-- like MUL-1918 instead of just preventing future regressions.
UPDATE agent_task_queue
SET failure_reason = 'api_invalid_request'
WHERE status = 'failed'
  AND COALESCE(failure_reason, '') = 'agent_error'
  AND error ILIKE '%400%'
  AND error ILIKE '%invalid_request_error%';
