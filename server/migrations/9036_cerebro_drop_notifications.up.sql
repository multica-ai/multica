-- CEREBRO-PATCH(drop-notifications-table): FIR-1914 removes the obsolete durable notifications table.
DROP TRIGGER IF EXISTS trg_notifications_dedup ON notifications;
DROP FUNCTION IF EXISTS suppress_duplicate_notification();
DROP TABLE IF EXISTS notifications;
