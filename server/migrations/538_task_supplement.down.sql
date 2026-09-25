DROP TRIGGER IF EXISTS trg_settle_terminal_task_supplements ON agent_task_queue;
DROP FUNCTION IF EXISTS settle_terminal_task_supplements();
DROP TABLE IF EXISTS task_supplement;
DROP TABLE IF EXISTS task_supplement_capability;
