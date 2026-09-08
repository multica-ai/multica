ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS session_model            TEXT,
    ADD COLUMN IF NOT EXISTS session_model_updated_at TIMESTAMPTZ;

ALTER TABLE chat_session
    ADD COLUMN IF NOT EXISTS session_model            TEXT,
    ADD COLUMN IF NOT EXISTS session_model_updated_at TIMESTAMPTZ;
