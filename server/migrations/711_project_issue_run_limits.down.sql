DROP TRIGGER enforce_issue_run_limits ON agent_task_queue;
DROP FUNCTION enforce_issue_run_limits();
ALTER TABLE project
    DROP COLUMN max_runs_per_issue,
    DROP COLUMN max_runs_per_agent_per_issue;
