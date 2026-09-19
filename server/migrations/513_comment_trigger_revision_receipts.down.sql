DROP TABLE IF EXISTS agent_task_comment_delivery_snapshot;

DROP TABLE IF EXISTS comment_trigger_delivery_receipt;

ALTER TABLE comment_trigger_outbox
    DROP COLUMN IF EXISTS comment_trigger_revision;

ALTER TABLE comment
    DROP COLUMN IF EXISTS trigger_revision;
