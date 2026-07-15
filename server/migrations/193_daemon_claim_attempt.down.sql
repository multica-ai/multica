ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS claim_attempt_ordinal,
    DROP COLUMN IF EXISTS claim_attempt_id;

DROP TABLE IF EXISTS daemon_claim_attempt;
