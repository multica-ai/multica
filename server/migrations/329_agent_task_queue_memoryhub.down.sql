ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS memory_gate_state,
    DROP COLUMN IF EXISTS memory_gate_error_code,
    DROP COLUMN IF EXISTS memory_gate_evidence_ref,
    DROP COLUMN IF EXISTS memory_gate_next_wakeup,
    DROP COLUMN IF EXISTS memory_gate_lease_id,
    DROP COLUMN IF EXISTS memory_gate_lease_expires_at,
    DROP COLUMN IF EXISTS memoryhub_run_id,
    DROP COLUMN IF EXISTS execution_id,
    DROP COLUMN IF EXISTS memory_policy,
    DROP COLUMN IF EXISTS memory_attachment_ref,
    DROP COLUMN IF EXISTS review_policy,
    DROP COLUMN IF EXISTS reviewer_agent_id,
    DROP COLUMN IF EXISTS review_of_execution_id;
