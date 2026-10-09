-- Receipt state follows the terminal transition atomically. This does not
-- delete dependents: issue/workspace/comment deletion remains application-owned.
DROP TRIGGER IF EXISTS trg_settle_terminal_task_supplements ON agent_task_queue;
CREATE TRIGGER trg_settle_terminal_task_supplements
AFTER UPDATE OF status ON agent_task_queue
FOR EACH ROW
WHEN (current_setting('multica.workspace_teardown', true) IS DISTINCT FROM 'on')
EXECUTE FUNCTION settle_terminal_task_supplements();
