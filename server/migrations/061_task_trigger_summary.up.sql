-- Snapshot the trigger context (comment text, autopilot title, etc) into
-- the task row at creation time. Lets every task row self-describe across
-- surfaces (issue detail Execution log, agent activity tooltip, inbox)
-- without joining back to the originating row, and survives later edits
-- or deletes of that source.
ALTER TABLE agent_task_queue ADD COLUMN trigger_summary TEXT;
