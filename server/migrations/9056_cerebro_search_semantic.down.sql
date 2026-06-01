DROP TRIGGER IF EXISTS trg_cerebro_search_enqueue_issue ON issue;
DROP TRIGGER IF EXISTS trg_cerebro_search_enqueue_comment ON comment;
DROP TRIGGER IF EXISTS trg_cerebro_search_drop_issue_embedding ON issue;
DROP TRIGGER IF EXISTS trg_cerebro_search_drop_comment_embedding ON comment;

DROP FUNCTION IF EXISTS cerebro_search_enqueue_issue();
DROP FUNCTION IF EXISTS cerebro_search_enqueue_comment();
DROP FUNCTION IF EXISTS cerebro_search_drop_issue_embedding();
DROP FUNCTION IF EXISTS cerebro_search_drop_comment_embedding();

DROP INDEX IF EXISTS idx_cerebro_search_embedding_queue_enqueued;
DROP INDEX IF EXISTS uq_cerebro_search_embedding_queue_target;
DROP TABLE IF EXISTS cerebro_search_embedding_queue;

DROP INDEX IF EXISTS idx_cerebro_search_embedding_vec;
DROP INDEX IF EXISTS idx_cerebro_search_embedding_ws;
DROP TABLE IF EXISTS cerebro_search_embedding;
