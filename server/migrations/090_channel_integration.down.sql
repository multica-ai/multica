DROP INDEX IF EXISTS idx_channel_reply_context_expiry;
DROP TABLE IF EXISTS channel_reply_context;

DROP INDEX IF EXISTS idx_channel_action_proposal_expiry;
DROP INDEX IF EXISTS idx_channel_action_proposal_lookup;
DROP TABLE IF EXISTS channel_action_proposal;

DROP INDEX IF EXISTS idx_channel_action_result_event;
DROP TABLE IF EXISTS channel_action_result;

DROP INDEX IF EXISTS idx_channel_outbound_notification_cleanup;
DROP INDEX IF EXISTS idx_channel_outbound_notification_processing;
DROP INDEX IF EXISTS idx_channel_outbound_notification_connection;
DROP INDEX IF EXISTS idx_channel_outbound_notification_due;
DROP TABLE IF EXISTS channel_outbound_notification;

DROP INDEX IF EXISTS idx_channel_inbound_event_connection_conversation;
DROP INDEX IF EXISTS idx_channel_inbound_event_waiting_user_expiry;
DROP INDEX IF EXISTS idx_channel_inbound_event_waiting_agent;
DROP INDEX IF EXISTS idx_channel_inbound_event_processing;
DROP INDEX IF EXISTS idx_channel_inbound_event_claim;
DROP TABLE IF EXISTS channel_inbound_event;

DROP TABLE IF EXISTS channel_conversation;

DROP INDEX IF EXISTS idx_channel_inbound_event_dedup_retryable;
DROP INDEX IF EXISTS idx_channel_inbound_event_dedup_processed_at;
DROP TABLE IF EXISTS channel_inbound_event_dedup;

DROP INDEX IF EXISTS idx_channel_bind_token_connection;
DROP INDEX IF EXISTS idx_channel_bind_token_unconsumed;
DROP TABLE IF EXISTS channel_bind_token;

DROP INDEX IF EXISTS idx_channel_chat_binding_agent;
DROP INDEX IF EXISTS idx_channel_chat_binding_default_project;
DROP INDEX IF EXISTS idx_channel_chat_binding_workspace_connection;
DROP INDEX IF EXISTS idx_channel_chat_binding_primary_per_connection_ws;
DROP TABLE IF EXISTS channel_chat_binding;

DROP TABLE IF EXISTS channel_user_binding;

DROP INDEX IF EXISTS idx_channel_connection_provider_default;
DROP TABLE IF EXISTS channel_connection;
