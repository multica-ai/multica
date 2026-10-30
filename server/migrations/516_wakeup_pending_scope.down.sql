-- Disable/drain wakeups before rolling back; ordinary task scopes are unchanged.
CREATE OR REPLACE FUNCTION set_agent_task_comment_thread() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 NEW.comment_thread_id := comment_thread_root_id(NEW.trigger_comment_id);
 RETURN NEW;
END
$$;
