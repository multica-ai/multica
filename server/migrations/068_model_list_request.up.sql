-- Persistent model-list request store. Replaces the previous in-memory
-- ModelListStore so that the POST (create) and GET (poll) endpoints work
-- correctly across multiple server instances.

CREATE TABLE model_list_request (
    id          TEXT PRIMARY KEY,
    runtime_id  UUID NOT NULL REFERENCES agent_runtime(id) ON DELETE CASCADE,
    status      TEXT NOT NULL DEFAULT 'pending',
    models      JSONB,
    supported   BOOLEAN NOT NULL DEFAULT true,
    error       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- PopPending needs to find the oldest pending row for a given runtime fast.
CREATE INDEX idx_model_list_request_runtime_pending
    ON model_list_request (runtime_id, created_at)
    WHERE status = 'pending';
