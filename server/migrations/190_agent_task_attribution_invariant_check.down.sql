ALTER TABLE agent_task_queue
    DROP CONSTRAINT IF EXISTS agent_task_queue_accountable_matches_originator;
