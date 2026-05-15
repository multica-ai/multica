ALTER TABLE agent_task_queue
  DROP COLUMN IF EXISTS claim_token,
  DROP COLUMN IF EXISTS claim_expires_at;
