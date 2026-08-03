CREATE INDEX CONCURRENTLY child_done_transition_queue_idx
    ON child_done_transition (available_at, transition_at)
    WHERE status = 'queued';
