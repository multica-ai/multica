CREATE INDEX CONCURRENTLY issue_wakeup_due_idx ON issue_wakeup(next_fire_at) WHERE enabled;
