CREATE INDEX CONCURRENTLY idx_wakeup_event_issue ON issue_wakeup(issue_id) WHERE enabled AND kind='event';
