DROP TABLE IF EXISTS comment_trigger_delivery_receipt;

ALTER TABLE comment_trigger_outbox
    DROP COLUMN IF EXISTS comment_trigger_revision;

ALTER TABLE comment
    DROP COLUMN IF EXISTS trigger_revision;
