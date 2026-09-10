CREATE UNIQUE INDEX CONCURRENTLY idx_issue_transition_revision ON issue_transition(issue_id, issue_revision_after);
