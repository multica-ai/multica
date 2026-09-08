DROP TRIGGER IF EXISTS controlled_launch_guard ON agent_task_queue;
DROP FUNCTION IF EXISTS enforce_controlled_launch();
DROP TRIGGER IF EXISTS issue_controller_guard ON issue;
DROP FUNCTION IF EXISTS enforce_issue_controller();
