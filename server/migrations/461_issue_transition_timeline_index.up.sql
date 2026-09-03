CREATE INDEX CONCURRENTLY idx_issue_transition_timeline ON issue_transition(issue_id, created_at DESC, id DESC);
