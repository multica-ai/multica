CREATE UNIQUE INDEX CONCURRENTLY comment_child_done_barrier_uidx
    ON comment (issue_id, child_done_barrier_key)
    WHERE child_done_barrier_key IS NOT NULL;
