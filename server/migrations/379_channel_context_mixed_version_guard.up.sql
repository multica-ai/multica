-- Snapshot only channel tasks that may still execute after this migration.
-- Terminal historical rows remain NULL and use the generation-1 read fallback;
-- rewriting them would create unnecessary table churn during deployment.
UPDATE agent_task_queue AS task
SET channel_context_revision = 1
WHERE task.channel_context_revision IS NULL
  AND task.chat_session_id IS NOT NULL
  AND task.status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred')
  AND (
      EXISTS (
          SELECT 1
          FROM channel_chat_session_binding AS binding
          WHERE binding.chat_session_id = task.chat_session_id
      )
      OR EXISTS (
          SELECT 1
          FROM chat_message AS message
          WHERE message.task_id = task.id
            AND message.channel_ingested
      )
  );

-- A channel message can only become input to a task from the same context
-- generation. This is a durable data invariant, not an old-writer shim: the
-- supported deployment strategy stops the previous server before starting the
-- new one, while adjacent-release rollback relies on nullable columns and the
-- generation-1 fallback.
CREATE OR REPLACE FUNCTION enforce_channel_message_task_context_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    task_revision BIGINT;
BEGIN
    IF OLD.task_id IS NOT NULL
       OR NEW.task_id IS NULL
       OR NEW.role <> 'user' THEN
        RETURN NEW;
    END IF;

    SELECT COALESCE(task.channel_context_revision, 1)
    INTO task_revision
    FROM agent_task_queue AS task
    WHERE task.id = NEW.task_id;

    IF task_revision IS NOT NULL
       AND COALESCE(NEW.channel_context_revision, 1) <> task_revision THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE TRIGGER trg_enforce_channel_message_task_context_revision
BEFORE UPDATE OF task_id ON chat_message
FOR EACH ROW
WHEN (OLD.task_id IS NULL AND NEW.task_id IS NOT NULL AND NEW.role = 'user')
EXECUTE FUNCTION enforce_channel_message_task_context_revision();
