-- Durable receipt for the explicitly opt-in corrective dispatch operation.
-- Issue deletion owns its explicit cleanup; no foreign key is used.
CREATE TABLE issue_rework_dispatch_receipt (
    workspace_id uuid NOT NULL,
    issue_id uuid NOT NULL,
    operation_key text NOT NULL,
    agent_id uuid NOT NULL,
    payload_sha256 text NOT NULL,
    instruction text NOT NULL,
    task_id uuid NOT NULL,
    source_task_id uuid NOT NULL,
    actor_user_id uuid,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
