-- Freeze the channel-native SENDER of a trigger alongside its message id, so
-- an outbound reply can @-mention exactly the account that asked (#8234).
--
-- The first cut resolved the mention by looking the member's binding back up
-- at send time (Multica user -> channel_user_binding). That is not sound:
-- channel_user_binding is unique on (installation_id, channel_user_id), NOT on
-- (installation_id, multica_user_id), and the redeem path plain-INSERTs a
-- second row when a member binds a second platform account on the same
-- installation. With two rows the reverse lookup picks an arbitrary one, so a
-- reply to account B could mention account A.
--
-- The sender id is per-trigger data, exactly like last_message_id, so it is
-- recorded and snapshotted through the same path: refreshed on every inbound
-- turn, then frozen per task into channel_task_delivery when the run is
-- created. No index: both columns are only ever read via their table's
-- existing key (chat_session_id / task_id).
--
-- Nullable with no backfill. Rows written before this migration mention
-- nobody, which is the same degradation as an unbound sender.
ALTER TABLE channel_chat_session_binding
    ADD COLUMN IF NOT EXISTS last_sender_id TEXT;

ALTER TABLE channel_task_delivery
    ADD COLUMN IF NOT EXISTS channel_sender_id TEXT;
