-- Channel Chat route migration.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM channel_chat_session_binding AS binding
        LEFT JOIN chat_session AS session ON session.id = binding.chat_session_id
        WHERE binding.retired_at IS NOT NULL
           OR session.explicitly_created_at IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'cannot roll back channel chat routes after /new has created a channel Chat';
    END IF;
END $$;

DROP TABLE IF EXISTS channel_outbound_message;
