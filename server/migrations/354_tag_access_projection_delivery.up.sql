CREATE TABLE tag_access_projection_delivery (
    vibes_workspace_id TEXT NOT NULL,
    authority_version BIGINT NOT NULL,
    delivery_kind TEXT NOT NULL,
    authority_assertion_id TEXT NOT NULL DEFAULT '',
    payload_digest BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT tag_access_projection_delivery_version_check CHECK (authority_version > 0),
    CONSTRAINT tag_access_projection_delivery_kind_check CHECK (delivery_kind IN ('incremental', 'snapshot', 'reconcile')),
    CONSTRAINT tag_access_projection_delivery_digest_check CHECK (octet_length(payload_digest) = 32)
);
