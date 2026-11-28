-- Adds the `ask_user_question` comment type: an agent posts a structured
-- multiple-choice question at a specific human (target_user); that user picks
-- one option and confirms, which posts a reply back to the agent (source_user)
-- so it can continue. The structured payload (question, options, answer state)
-- lives in a new comment.metadata JSONB column; content still holds a
-- human-readable Markdown fallback so old clients / mobile degrade gracefully.

-- 1) Extend the type CHECK. The base constraint is the unnamed inline CHECK
--    from 001_init.up.sql, which Postgres names `comment_type_check`.
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_type_check;
ALTER TABLE comment ADD CONSTRAINT comment_type_check
    CHECK (type IN ('comment', 'status_change', 'progress_update', 'system', 'ask_user_question'));

-- 2) Structured payload column. Mirrors the issue.metadata shape: JSONB
--    NOT NULL DEFAULT '{}', object-only, size-capped. Handler-level validation
--    enforces the ask_user_question schema; these CHECKs are defense-in-depth
--    against direct SQL / migrations that bypass the API.
ALTER TABLE comment ADD COLUMN metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE comment ADD CONSTRAINT comment_metadata_is_object
    CHECK (jsonb_typeof(metadata) = 'object');

ALTER TABLE comment ADD CONSTRAINT comment_metadata_size_limit
    CHECK (pg_column_size(metadata) <= 8192);
