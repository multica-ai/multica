DROP TABLE IF EXISTS knowledge_usage_event;

DROP INDEX IF EXISTS idx_knowledge_feedback_task;
DROP INDEX IF EXISTS idx_knowledge_retrieval_event_task;

ALTER TABLE knowledge_feedback
    DROP COLUMN IF EXISTS agent_task_id;

ALTER TABLE knowledge_injection_event
    DROP COLUMN IF EXISTS discarded_reason,
    DROP COLUMN IF EXISTS token_budget,
    DROP COLUMN IF EXISTS injection_reason,
    DROP COLUMN IF EXISTS score,
    DROP COLUMN IF EXISTS rank;

ALTER TABLE knowledge_retrieval_event
    DROP COLUMN IF EXISTS result_scores,
    DROP COLUMN IF EXISTS query_source,
    DROP COLUMN IF EXISTS agent_task_id;
