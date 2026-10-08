CREATE UNIQUE INDEX CONCURRENTLY idx_seat_capacity_outbox_operation_token
    ON seat_capacity_outbox(operation_token);
