CREATE TABLE tag_http_assertion_replay (
    issuer TEXT NOT NULL,
    audience TEXT NOT NULL,
    request_id TEXT NOT NULL,
    nonce TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
