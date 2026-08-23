ALTER TABLE attachment DROP COLUMN IF EXISTS source_context_id;
DROP TABLE IF EXISTS issue_source_context_object_intent;
DROP TABLE IF EXISTS issue_source_context;
