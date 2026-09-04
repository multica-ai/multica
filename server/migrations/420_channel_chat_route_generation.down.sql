-- Channel Chat route migration.
DROP TABLE IF EXISTS channel_task_delivery;

ALTER TABLE chat_session
    DROP COLUMN IF EXISTS explicitly_created_at;

ALTER TABLE channel_chat_session_binding
    DROP COLUMN IF EXISTS history_boundary_pending,
    DROP COLUMN IF EXISTS history_end_message_id,
    DROP COLUMN IF EXISTS history_start_message_id,
    DROP COLUMN IF EXISTS retired_at,
    DROP COLUMN IF EXISTS route_revision;
