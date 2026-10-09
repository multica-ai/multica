-- Task terminal transitions now settle pending supplements explicitly in the
-- application transaction. Remove the cross-table trigger so task completion
-- no longer acquires task_supplement locks through database-side cascade logic.
DROP TRIGGER IF EXISTS trg_settle_terminal_task_supplements ON agent_task_queue;
DROP FUNCTION IF EXISTS settle_terminal_task_supplements();
