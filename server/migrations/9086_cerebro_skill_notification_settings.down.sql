ALTER TABLE skill
    DROP COLUMN IF EXISTS notify_change_requests,
    DROP COLUMN IF EXISTS notify_forks,
    DROP COLUMN IF EXISTS notify_agent_assigned;
