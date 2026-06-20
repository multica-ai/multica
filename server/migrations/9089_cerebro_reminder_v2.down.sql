-- CEREBRO-PATCH(cerebro-reminder): FIR-394 v2 down — revert to the narrow v1
-- shape. Reminders to other recipients / non-message anchors are dropped first
-- (v1 cannot represent them), then the generalising columns are removed.
DELETE FROM cerebro_reminder
WHERE recipient_type <> 'member'
   OR recipient_id <> user_id
   OR anchor_type NOT IN ('comment', 'none');

DROP INDEX IF EXISTS idx_cerebro_reminder_recipient;
CREATE INDEX IF NOT EXISTS idx_cerebro_reminder_user
    ON cerebro_reminder (workspace_id, user_id, status, remind_at);

ALTER TABLE cerebro_reminder
    DROP CONSTRAINT IF EXISTS cerebro_reminder_recipient_type_check,
    DROP CONSTRAINT IF EXISTS cerebro_reminder_anchor_type_check,
    DROP COLUMN IF EXISTS creator_id,
    DROP COLUMN IF EXISTS recipient_type,
    DROP COLUMN IF EXISTS recipient_id,
    DROP COLUMN IF EXISTS anchor_type,
    DROP COLUMN IF EXISTS project_id;
