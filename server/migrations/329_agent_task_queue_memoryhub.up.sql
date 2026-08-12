-- agent_task_queue MemoryHub columns (v1.3 A1 gate fields + v1.2 execution
-- reference + memory policy/attachment ref + review lineage fields).
ALTER TABLE agent_task_queue
    ADD COLUMN memory_gate_state TEXT,
    ADD COLUMN memory_gate_error_code TEXT,
    ADD COLUMN memory_gate_evidence_ref TEXT,
    ADD COLUMN memory_gate_next_wakeup TIMESTAMPTZ,
    ADD COLUMN memory_gate_lease_id TEXT,
    ADD COLUMN memory_gate_lease_expires_at TIMESTAMPTZ,
    ADD COLUMN memoryhub_run_id TEXT,
    ADD COLUMN execution_id UUID,
    ADD COLUMN memory_policy TEXT NOT NULL DEFAULT 'optional',
    ADD COLUMN memory_attachment_ref TEXT,
    ADD COLUMN review_policy TEXT,
    ADD COLUMN reviewer_agent_id UUID,
    ADD COLUMN review_of_execution_id UUID;
