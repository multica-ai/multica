CREATE INDEX CONCURRENTLY child_done_transition_group_queue_idx
    ON child_done_transition (group_id, available_at)
    WHERE status = 'queued';
