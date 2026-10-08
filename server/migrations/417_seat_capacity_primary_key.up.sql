ALTER TABLE seat_capacity_outbox
    ADD CONSTRAINT seat_capacity_outbox_pkey
    PRIMARY KEY USING INDEX idx_seat_capacity_outbox_operation_token;
