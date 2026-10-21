-- SHAR-146 (B1): let a member choose which WeCom bot receives their task
-- notifications, and stop a newly-bound bot from silently hijacking every
-- notification.
--
-- Root cause: outbound notification addressing reads
-- FindChannelBindingForMember, which breaks ties between multiple bindings of
-- the same channel_type by bound_at DESC (newest wins). A member who binds a
-- second bot therefore re-points ALL task notifications to it immediately.
--
-- The fix has two halves:
--   1. an explicit per-member notification target (this migration), one at
--      most per (workspace, member, channel_type), enforced by a partial
--      unique index;
--   2. the notification lookup preferring that target and falling back to the
--      OLDEST binding when none is set (FindNotificationBindingForMember,
--      shipped with the query + caller change in the same commit).
--
-- Every existing row defaults to FALSE, so after this migration alone a
-- multi-bound member's notifications go to their oldest binding — the one
-- they had before the newest bot hijacked them — until they explicitly pick
-- a target. New bindings never write the flag (the INSERT in
-- CreateChannelUserBinding omits it), so "bind a bot" no longer moves
-- notifications.

ALTER TABLE channel_user_binding
    ADD COLUMN is_notification_target BOOLEAN NOT NULL DEFAULT FALSE;

-- One notification target per member per channel_type in a workspace. Partial:
-- only rows flagged as targets participate, so ordinary bindings stay unlimited.
CREATE UNIQUE INDEX idx_channel_user_binding_notification_target
    ON channel_user_binding (workspace_id, multica_user_id, channel_type)
    WHERE is_notification_target;
