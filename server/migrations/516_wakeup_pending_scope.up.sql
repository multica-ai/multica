-- Preserve the existing unique index and retry ON CONFLICT contract during a
-- rolling deployment. For wakeup runs the scheduling scope is the configuration
-- id; trigger_comment_id still holds the real result-delivery thread.
CREATE OR REPLACE FUNCTION set_agent_task_comment_thread() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 NEW.comment_thread_id := COALESCE(NULLIF(NEW.context->>'wakeup_id','')::uuid,comment_thread_root_id(NEW.trigger_comment_id));
 RETURN NEW;
END
$$;
