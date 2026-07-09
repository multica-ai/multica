CREATE INDEX IF NOT EXISTS idx_activity_log_squad_no_action_task
    ON activity_log (issue_id, actor_id, ((details->>'task_id')))
    WHERE actor_type = 'agent'
      AND action = 'squad_leader_evaluated'
      AND details->>'outcome' = 'no_action';
