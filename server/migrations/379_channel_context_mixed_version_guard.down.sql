DROP TRIGGER IF EXISTS trg_sync_channel_generation_pending_fresh ON channel_chat_session_binding;
DROP FUNCTION IF EXISTS sync_channel_generation_pending_fresh();

DROP TRIGGER IF EXISTS trg_enforce_channel_message_task_context_revision ON chat_message;
DROP FUNCTION IF EXISTS enforce_channel_message_task_context_revision();

DROP TRIGGER IF EXISTS trg_stamp_channel_task_context_revision ON agent_task_queue;
DROP FUNCTION IF EXISTS stamp_channel_task_context_revision();

DROP TRIGGER IF EXISTS trg_stamp_channel_message_context_revision ON chat_message;
DROP FUNCTION IF EXISTS stamp_channel_message_context_revision();
