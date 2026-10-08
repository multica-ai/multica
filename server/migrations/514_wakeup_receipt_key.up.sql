CREATE UNIQUE INDEX CONCURRENTLY issue_wakeup_receipt_key_idx ON issue_wakeup_receipt(wakeup_id,revision,event_key);
