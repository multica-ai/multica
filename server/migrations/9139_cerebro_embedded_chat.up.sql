-- FIR-2835: Cerebro-owned classification avoids changing upstream chat_session.
CREATE TABLE IF NOT EXISTS cerebro_chat_session_context (
    chat_session_id uuid PRIMARY KEY REFERENCES chat_session(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('api')),
    source text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS cerebro_chat_session_context_kind_idx
    ON cerebro_chat_session_context (kind, source);
