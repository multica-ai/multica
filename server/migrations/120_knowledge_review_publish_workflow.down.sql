DROP TABLE IF EXISTS knowledge_publish_target;

ALTER TABLE knowledge_item
    DROP COLUMN IF EXISTS deprecated_at,
    DROP COLUMN IF EXISTS updated_by;
