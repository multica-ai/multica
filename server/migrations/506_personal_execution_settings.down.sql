ALTER TABLE agent_runtime_preference
    DROP CONSTRAINT personal_execution_model,
    DROP COLUMN max_concurrent_tasks,
    DROP COLUMN model,
    DROP COLUMN model_mode;
