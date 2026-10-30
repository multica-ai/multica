CREATE TABLE agent_task_code_changes (
    task_id uuid NOT NULL,
    runtime_id uuid,
    dispatched_at timestamptz,
    snapshot bytea NOT NULL,
    snapshot_digest text NOT NULL,
    file_count integer NOT NULL
);
