SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

DROP TRIGGER IF EXISTS trg_chat_message_search_index_change ON chat_message;
DROP TRIGGER IF EXISTS trg_chat_session_search_index_change ON chat_session;
DROP FUNCTION IF EXISTS record_chat_message_search_index_change();

-- Rows the chat triggers wrote would violate the narrower constraint.
DELETE FROM search_index_change WHERE entity_type IN ('chat_session', 'chat_message');

ALTER TABLE search_index_change DROP CONSTRAINT search_index_change_entity_type_check;
ALTER TABLE search_index_change ADD CONSTRAINT search_index_change_entity_type_check
    CHECK (entity_type IN ('issue', 'comment', 'project'));
