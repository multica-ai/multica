-- Delegated-failure recovery repeatedly counts attempts for one failed task and
-- selects its newest attempt by created_at/id. Keep this partial because
-- agent_task_queue is write-heavy and only recovery tasks need the lookup.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_delegated_failure_evidence
    ON agent_task_queue (trigger_evidence_ref_id, created_at DESC, id DESC)
    WHERE trigger_evidence_kind = 'delegated_failure';
