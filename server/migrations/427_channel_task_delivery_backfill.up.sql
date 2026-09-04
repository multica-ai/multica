-- Channel Chat route migration.
INSERT INTO channel_task_delivery (
    task_id, binding_id, installation_id, channel_type, channel_chat_id, chat_type,
    channel_message_id, channel_thread_id, route_revision, config
)
SELECT
    task.id, binding.id, binding.installation_id, binding.channel_type,
    binding.channel_chat_id, binding.chat_type, binding.last_message_id, binding.last_thread_id,
    binding.route_revision, binding.config
FROM agent_task_queue AS task
JOIN channel_chat_session_binding AS binding
  ON binding.chat_session_id = task.chat_session_id
WHERE task.status IN ('queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory')
  AND EXISTS (
      SELECT 1 FROM chat_message AS message
      WHERE message.task_id = COALESCE(task.chat_input_task_id, task.id)
        AND message.role = 'user'
        AND message.channel_ingested
  )
ON CONFLICT (task_id) DO NOTHING;
