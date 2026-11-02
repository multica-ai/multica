DROP TRIGGER IF EXISTS trg_settle_terminal_task_supplements ON agent_task_queue;
CREATE TRIGGER trg_settle_terminal_task_supplements
AFTER UPDATE OF status ON agent_task_queue
FOR EACH ROW EXECUTE FUNCTION settle_terminal_task_supplements();
