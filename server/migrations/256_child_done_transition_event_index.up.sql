CREATE UNIQUE INDEX CONCURRENTLY child_done_transition_event_uidx
    ON child_done_transition (child_issue_id, transition_at, terminal_status);
