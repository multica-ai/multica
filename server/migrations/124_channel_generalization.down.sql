-- Reverse 124_channel_generalization.up.sql. The lark_* tables were left
-- in place by the up migration, so rolling back only needs to drop the
-- channel_* tables that were added.
DROP TABLE IF EXISTS channel_binding_token;
DROP TABLE IF EXISTS channel_outbound_card_message;
DROP TABLE IF EXISTS channel_inbound_audit;
DROP TABLE IF EXISTS channel_inbound_message_dedup;
DROP TABLE IF EXISTS channel_chat_session_binding;
DROP TABLE IF EXISTS channel_user_binding;
DROP TABLE IF EXISTS channel_installation;
