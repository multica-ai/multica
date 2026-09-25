CREATE INDEX CONCURRENTLY idx_seat_capacity_outbox_due
    ON seat_capacity_outbox(next_attempt_at, created_at)
    WHERE dead_lettered_at IS NULL;
