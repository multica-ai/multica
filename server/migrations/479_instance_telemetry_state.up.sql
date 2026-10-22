-- One deployment identity and at most one durable pending snapshot. No user,
-- workspace or runtime identifier is referenced from this table.
CREATE TABLE instance_telemetry_state (
    singleton             BOOLEAN NOT NULL DEFAULT true CHECK (singleton),
    instance_id           UUID NOT NULL DEFAULT gen_random_uuid(),
    last_successful_day   DATE,
    pending_day           DATE,
    pending_body          BYTEA,
    next_attempt_at       TIMESTAMPTZ,
    attempt_count         INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (pending_day IS NULL AND pending_body IS NULL AND next_attempt_at IS NULL AND attempt_count = 0)
        OR
        (pending_day IS NOT NULL AND pending_body IS NOT NULL AND next_attempt_at IS NOT NULL)
    )
);

INSERT INTO instance_telemetry_state (singleton) VALUES (true);
