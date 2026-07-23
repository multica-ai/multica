CREATE UNIQUE INDEX IF NOT EXISTS inbox_item_eval_advisory_notification_key_unique
    ON inbox_item (workspace_id, recipient_type, recipient_id, (details->>'notification_key'))
    WHERE type = 'eval_advisory_failed' AND details->>'notification_key' IS NOT NULL;
