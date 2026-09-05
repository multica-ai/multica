CREATE TABLE IF NOT EXISTS cerebra_model_unavailability (
    runtime_id  TEXT        NOT NULL,
    model       TEXT        NOT NULL,
    marked_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ttl_seconds INT         NOT NULL DEFAULT 3600,
    PRIMARY KEY (runtime_id, model)
);
