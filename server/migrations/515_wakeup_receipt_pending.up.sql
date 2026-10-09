CREATE INDEX CONCURRENTLY issue_wakeup_receipt_pending_idx ON issue_wakeup_receipt(wakeup_id,created_at) WHERE processed_at IS NULL;
