-- Reverse of 573 up: drop the per-member notification-target flag.
-- The unique partial index goes first (it depends on the column).
DROP INDEX IF EXISTS idx_channel_user_binding_notification_target;

ALTER TABLE channel_user_binding
    DROP COLUMN IF EXISTS is_notification_target;
