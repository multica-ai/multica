CREATE INDEX CONCURRENTLY issue_wakeup_receipt_expiry_idx ON issue_wakeup_receipt(processed_at,id) WHERE processed_at IS NOT NULL;
