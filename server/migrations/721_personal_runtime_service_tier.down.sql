ALTER TABLE agent_task_queue
    DROP COLUMN effective_service_tier;

ALTER TABLE agent_user_runtime_config
    DROP COLUMN service_tier;
