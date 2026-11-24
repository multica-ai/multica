ALTER TABLE chat_session
  ADD COLUMN model_override text,
  ADD COLUMN thinking_level_override text;

ALTER TABLE agent_task_queue
  ADD COLUMN chat_model text,
  ADD COLUMN chat_thinking_level text;
