CREATE TABLE corpus_transfer_ack (
    workspace_id    UUID NOT NULL,
    transfer_id     UUID NOT NULL,
    sink_id         TEXT NOT NULL CHECK (char_length(sink_id) BETWEEN 1 AND 255),
    confirmed_sha256 TEXT NOT NULL CHECK (confirmed_sha256 ~ '^[0-9a-f]{64}$'),
    acknowledged_by UUID NOT NULL,
    acknowledged_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
