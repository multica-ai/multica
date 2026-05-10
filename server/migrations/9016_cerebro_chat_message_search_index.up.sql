-- GIN index on LOWER(chat_message.content) for LIKE '%keyword%' queries (pg_bigm).
-- Mirrors 036_search_index_lower — pg_bigm 1.2 (RDS) only uses the GIN index
-- when the queried expression matches the indexed expression exactly. The
-- SearchChatSessions handler queries `LOWER(m.content) LIKE $1`, so the
-- index must be on `LOWER(content)`, not raw `content`. Indexing raw content
-- (the 033 pattern) leaves the query on a seq scan.
-- Only created when pg_bigm is installed; envs without the extension fall
-- back to seq scans.
DO $$
BEGIN
  CREATE INDEX IF NOT EXISTS idx_chat_message_content_bigm ON chat_message USING gin (LOWER(content) gin_bigm_ops);
EXCEPTION WHEN OTHERS THEN
  RAISE NOTICE 'skipping bigram index on chat_message (pg_bigm not installed)';
END
$$;
