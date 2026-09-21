DROP TRIGGER agent_task_comment_thread ON agent_task_queue;
DROP FUNCTION set_agent_task_comment_thread();
ALTER TABLE agent_task_queue DROP COLUMN comment_thread_id;
DROP FUNCTION comment_thread_root_id(uuid);
