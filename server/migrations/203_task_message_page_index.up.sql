-- Execution Log pagination (MUL-5122) orders and cursors task messages by the
-- stable (seq, id) tuple: seq is the daemon's intended order but is not unique
-- (a fresh-session retry can repeat seq values), so id breaks ties to give a
-- total order. This composite index serves both the ORDER BY seq DESC, id DESC
-- windowing and the (seq, id) row-comparison cursor so a 10,000-event Run pages
-- without a full scan. It is a superset of idx_task_message_task_id_seq, which
-- is left in place for the existing full-list / since queries. Keep this as the
-- migration's only statement: PostgreSQL rejects CREATE INDEX CONCURRENTLY
-- inside a transaction or multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_message_task_id_seq_id
    ON task_message (task_id, seq, id);
