-- Smart runtime: per-user service tier selection, stamped onto tasks the same
-- way as the rest of the personal configuration.
ALTER TABLE agent_user_runtime_config
    ADD COLUMN service_tier TEXT NOT NULL DEFAULT '';

ALTER TABLE agent_task_queue
    ADD COLUMN effective_service_tier TEXT;

UPDATE agent_task_queue t
SET effective_service_tier = a.service_tier
FROM agent a
WHERE a.id = t.agent_id
  AND t.effective_service_tier IS NULL;
