-- CEREBRO-PATCH(notifications-table): firtal notification schema migration
CREATE TABLE IF NOT EXISTS notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    recipient_id UUID NOT NULL,
    recipient_type TEXT NOT NULL CHECK (recipient_type IN ('member', 'agent')),
    type TEXT NOT NULL,
    reference_id UUID NOT NULL,
    reference_type TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_notifications_recipient_created_at
    ON notifications (workspace_id, recipient_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_notifications_unread_count
    ON notifications (workspace_id, recipient_id)
    WHERE read_at IS NULL;

CREATE OR REPLACE FUNCTION suppress_duplicate_notification()
RETURNS TRIGGER AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM notifications
        WHERE workspace_id = NEW.workspace_id
          AND recipient_id = NEW.recipient_id
          AND type = NEW.type
          AND reference_id = NEW.reference_id
          AND created_at >= NEW.created_at - interval '5 minutes'
    ) THEN
        RETURN NULL;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notifications_dedup ON notifications;
CREATE TRIGGER trg_notifications_dedup
    BEFORE INSERT ON notifications
    FOR EACH ROW
    EXECUTE FUNCTION suppress_duplicate_notification();
