CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS cursor_usage_event_claim_account_occurrence_uidx
    ON cursor_usage_event_claim (account_key, occurrence_key);
