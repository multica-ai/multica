DROP TRIGGER IF EXISTS workspace_claim_intake_control_initialize ON workspace;
DROP FUNCTION IF EXISTS initialize_workspace_claim_intake_control();

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS claim_consumer_id,
    DROP COLUMN IF EXISTS claim_intake_action_id,
    DROP COLUMN IF EXISTS claim_intake_generation;

DROP TABLE IF EXISTS workspace_claim_intake_action;
DROP TABLE IF EXISTS workspace_claim_intake_control;
