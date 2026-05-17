DROP INDEX IF EXISTS idx_channel_turn_waiting_task;
DROP INDEX IF EXISTS idx_channel_turn_status;
DROP INDEX IF EXISTS idx_channel_turn_outbound_message;
DROP INDEX IF EXISTS idx_channel_turn_conversation_created;
DROP INDEX IF EXISTS idx_channel_turn_inbound_message;
DROP INDEX IF EXISTS idx_channel_turn_inbound_event;
DROP TABLE IF EXISTS channel_turn;

DROP INDEX IF EXISTS idx_channel_message_entity_ref_workspace_key;
DROP INDEX IF EXISTS idx_channel_message_entity_ref_entity_id;
DROP INDEX IF EXISTS idx_channel_message_entity_ref_message;
DROP INDEX IF EXISTS idx_channel_message_entity_ref_unique_key;
DROP INDEX IF EXISTS idx_channel_message_entity_ref_unique_id;
DROP TABLE IF EXISTS channel_message_entity_ref;

DROP INDEX IF EXISTS idx_channel_message_reply_platform;
DROP INDEX IF EXISTS idx_channel_message_handoff;
DROP INDEX IF EXISTS idx_channel_message_thread_occurred;
DROP INDEX IF EXISTS idx_channel_message_conversation_occurred;
DROP INDEX IF EXISTS idx_channel_message_outbound_notification;
DROP INDEX IF EXISTS idx_channel_message_inbound_event;
DROP INDEX IF EXISTS idx_channel_message_event;
DROP INDEX IF EXISTS idx_channel_message_platform_message;
DROP TABLE IF EXISTS channel_message;

DROP INDEX IF EXISTS idx_channel_action_proposal_expiry;
DROP INDEX IF EXISTS idx_channel_action_proposal_lookup;
DROP TABLE IF EXISTS channel_action_proposal;

DROP INDEX IF EXISTS idx_channel_outbound_notification_cleanup;
DROP INDEX IF EXISTS idx_channel_outbound_notification_processing;
DROP INDEX IF EXISTS idx_channel_outbound_notification_connection;
DROP INDEX IF EXISTS idx_channel_outbound_notification_due;
DROP TABLE IF EXISTS channel_outbound_notification;

DROP INDEX IF EXISTS idx_channel_action_result_event;
DROP TABLE IF EXISTS channel_action_result;

DROP INDEX IF EXISTS idx_channel_processing_lock_stale;
DROP INDEX IF EXISTS idx_channel_processing_lock_active;
DROP TABLE IF EXISTS channel_processing_lock;

DROP INDEX IF EXISTS idx_channel_inbound_event_processing_key;
DROP INDEX IF EXISTS idx_channel_inbound_event_waiting_agent;
DROP INDEX IF EXISTS idx_channel_inbound_event_processing;
DROP INDEX IF EXISTS idx_channel_inbound_event_claim;
DROP TABLE IF EXISTS channel_inbound_event;

DROP INDEX IF EXISTS idx_channel_conversation_last_message;
DROP INDEX IF EXISTS idx_channel_conversation_workspace;
DROP INDEX IF EXISTS idx_channel_conversation_connection_chat;
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
