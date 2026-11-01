ALTER TABLE knowledge_link
    DROP COLUMN IF EXISTS error,
    DROP COLUMN IF EXISTS publish_requested,
    DROP COLUMN IF EXISTS pending_content,
    DROP COLUMN IF EXISTS source_url,
    DROP COLUMN IF EXISTS title;
