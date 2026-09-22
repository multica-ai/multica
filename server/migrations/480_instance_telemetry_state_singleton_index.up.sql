CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS instance_telemetry_state_singleton_uidx
    ON instance_telemetry_state (singleton);
