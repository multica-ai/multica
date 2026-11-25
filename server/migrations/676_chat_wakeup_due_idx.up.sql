-- Serves the dispatcher's scan for wakeups that are due.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_wakeup_due
    ON chat_wakeup (next_eval_at) WHERE status = 'active';
