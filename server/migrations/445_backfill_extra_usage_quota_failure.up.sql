-- Backfill Claude Code's exact extra-usage exhaustion wording into the quota
-- bucket added to taskfailure.Classify in WS-4612. Before this classifier
-- witness existed, the message had no other matching token and landed in
-- agent_error.unknown, which prevented the scheduled-autopilot resource retry
-- policy from recognizing historical failures consistently.
--
-- The bare message landed in agent_error.unknown. Daemon wrappers that append
-- the CLI exit status landed in agent_error.process_failure, and sufficiently
-- old rows can still carry the pre-taxonomy agent_error bucket. The phrase is
-- a provider-owned quota verdict, so all three source labels are safe to repair.
UPDATE agent_task_queue
SET failure_reason = 'agent_error.provider_quota_limit'
WHERE status = 'failed'
  AND failure_reason IN ('agent_error.unknown', 'agent_error.process_failure', 'agent_error')
  AND error ILIKE '%out of extra usage%';

-- Chat failures mirror the task reason and need the same historical label.
UPDATE chat_message
SET failure_reason = 'agent_error.provider_quota_limit'
WHERE failure_reason IN ('agent_error.unknown', 'agent_error.process_failure', 'agent_error')
  AND content ILIKE '%out of extra usage%';
