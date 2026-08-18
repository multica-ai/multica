-- During a rolling deployment, an older server still writes the pre-context
-- INSERT/UPDATE shapes. Stamp its new channel rows with the binding's current
-- revision and reject cross-revision message ownership at the database boundary.

CREATE OR REPLACE FUNCTION stamp_channel_message_context_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.channel_context_revision IS NULL
       AND NEW.role = 'user' THEN
        SELECT binding.context_revision
        INTO NEW.channel_context_revision
        FROM channel_chat_session_binding AS binding
        WHERE binding.chat_session_id = NEW.chat_session_id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE TRIGGER trg_stamp_channel_message_context_revision
BEFORE INSERT ON chat_message
FOR EACH ROW
WHEN (NEW.channel_context_revision IS NULL AND NEW.role = 'user')
EXECUTE FUNCTION stamp_channel_message_context_revision();

CREATE OR REPLACE FUNCTION stamp_channel_task_context_revision()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    source_task_id UUID;
BEGIN
    IF NEW.channel_context_revision IS NOT NULL OR NEW.chat_session_id IS NULL THEN
        RETURN NEW;
    END IF;

    source_task_id := COALESCE(
        NEW.retry_of_task_id,
        NEW.rerun_of_task_id,
        NEW.parent_task_id,
        NEW.chat_input_task_id
    );
    IF source_task_id IS NOT NULL THEN
        SELECT COALESCE(task.channel_context_revision, 1)
        INTO NEW.channel_context_revision
        FROM agent_task_queue AS task
        WHERE task.id = source_task_id;
    ELSE
        SELECT binding.context_revision
        INTO NEW.channel_context_revision
        FROM channel_chat_session_binding AS binding
        WHERE binding.chat_session_id = NEW.chat_session_id;
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE TRIGGER trg_stamp_channel_task_context_revision
BEFORE INSERT ON agent_task_queue
FOR EACH ROW
WHEN (NEW.channel_context_revision IS NULL AND NEW.chat_session_id IS NOT NULL)
EXECUTE FUNCTION stamp_channel_task_context_revision();

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

CREATE OR REPLACE FUNCTION sync_channel_generation_pending_fresh()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF OLD.pending_fresh AND NOT NEW.pending_fresh THEN
        UPDATE channel_chat_context_generation AS generation
        SET pending_fresh = FALSE
        WHERE generation.chat_session_id = NEW.chat_session_id
          AND generation.revision = NEW.context_revision;
    END IF;
    RETURN NULL;
END;
$$;

CREATE OR REPLACE TRIGGER trg_sync_channel_generation_pending_fresh
AFTER UPDATE OF pending_fresh ON channel_chat_session_binding
FOR EACH ROW
WHEN (OLD.pending_fresh AND NOT NEW.pending_fresh)
EXECUTE FUNCTION sync_channel_generation_pending_fresh();
