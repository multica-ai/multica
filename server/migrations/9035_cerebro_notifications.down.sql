-- CEREBRO-PATCH(notifications-table): firtal notification schema rollback
DROP TRIGGER IF EXISTS trg_notifications_dedup ON notifications;
DROP FUNCTION IF EXISTS suppress_duplicate_notification();
DROP INDEX IF EXISTS idx_notifications_unread_count;
DROP INDEX IF EXISTS idx_notifications_recipient_created_at;
DROP TABLE IF EXISTS notifications;
