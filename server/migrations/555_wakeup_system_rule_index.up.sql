CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_wakeup_system_rule_idx ON issue_wakeup(issue_id,system_rule) WHERE system_rule IS NOT NULL;
