ALTER TABLE chat_session
    DROP COLUMN IF EXISTS session_model_updated_at,
    DROP COLUMN IF EXISTS session_model;

ALTER TABLE issue
    DROP COLUMN IF EXISTS session_model_updated_at,
    DROP COLUMN IF EXISTS session_model;
