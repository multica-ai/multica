ALTER TABLE agent_task_queue
  DROP COLUMN chat_thinking_level,
  DROP COLUMN chat_model;

ALTER TABLE chat_session
  DROP COLUMN thinking_level_override,
  DROP COLUMN model_override;
