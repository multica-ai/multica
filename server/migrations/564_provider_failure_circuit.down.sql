DROP INDEX IF EXISTS idx_agent_task_origin_event_consumer;
UPDATE agent_task_queue AS task
SET trigger_evidence_kind = backup.trigger_evidence_kind,
    trigger_evidence_ref_id = backup.trigger_evidence_ref_id
FROM agent_task_origin_event_dedupe_backup backup
WHERE task.id = backup.task_id;
DROP TABLE IF EXISTS agent_task_origin_event_dedupe_backup;
DROP TABLE IF EXISTS provider_failure_circuit;
