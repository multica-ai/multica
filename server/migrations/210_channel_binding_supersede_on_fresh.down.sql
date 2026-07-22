-- Collapse back to one row per (installation_id, channel_chat_id): drop the
-- superseded (inactive) rows the supersede model accumulated, then restore the
-- plain UNIQUE constraint and remove the active flag.
DELETE FROM channel_chat_session_binding WHERE NOT active;

DROP INDEX IF EXISTS channel_chat_session_binding_active_chat_key;

ALTER TABLE channel_chat_session_binding
    ADD CONSTRAINT channel_chat_session_binding_installation_id_channel_chat_i_key
    UNIQUE (installation_id, channel_chat_id);

ALTER TABLE channel_chat_session_binding
    DROP COLUMN active;
