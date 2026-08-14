-- Durable idempotency ledger for the private Qianwen Skill polling bridge.
-- Only a digest of the normalized query is retained: the spoken request body
-- belongs in the existing chat transcript, not in a second content store.
--
-- There are deliberately no foreign keys. Installation revocation and
-- chat-session archive/deletion must not erase the request key: a later replay
-- of the same external request_id still has to be recognized even when its
-- presentation rows are retired. Installation lifecycle cleanup explicitly
-- removes orphaned ledger rows; GetQianwenRequestStatus separately requires an
-- active installation before it exposes any state.
CREATE TABLE qianwen_skill_request (
    installation_id  UUID NOT NULL,
    request_id       UUID NOT NULL,
    query_sha256     BYTEA NOT NULL,
    claim_token      UUID,
    claim_expires_at TIMESTAMPTZ,
    chat_session_id  UUID,
    task_id          UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (octet_length(query_sha256) = 32),
    CHECK ((claim_token IS NULL) = (claim_expires_at IS NULL))
);

COMMENT ON TABLE qianwen_skill_request IS
    'Durable Qianwen request idempotency ledger. No FKs by design: installation revocation and chat archive/deletion must not erase an accepted request key.';

COMMENT ON COLUMN qianwen_skill_request.query_sha256 IS
    'SHA-256 of the normalized request query; the plaintext lives only in chat_message.';

COMMENT ON COLUMN qianwen_skill_request.claim_token IS
    'Fencing token for the current submit owner; every successful claim or reclaim mints a new UUID.';

COMMENT ON COLUMN qianwen_skill_request.claim_expires_at IS
    'DB-clock lease deadline after which an unfinished request may be reclaimed.';
