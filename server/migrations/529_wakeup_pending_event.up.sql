CREATE UNIQUE INDEX CONCURRENTLY issue_wakeup_pending_event_idx ON issue_wakeup_receipt(wakeup_id,revision,coalesce_key) WHERE processed_at IS NULL AND coalesce_key IS NOT NULL;
