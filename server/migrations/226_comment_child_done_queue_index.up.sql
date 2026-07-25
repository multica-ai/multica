CREATE INDEX CONCURRENTLY comment_child_done_queue_idx
    ON comment (child_done_available_at, created_at)
    WHERE child_done_dispatch_status = 'queued';
