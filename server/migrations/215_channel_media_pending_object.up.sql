-- Intent ledger for channel media objects (two-system atomicity, PR #5580).
-- A row is written BEFORE the object is uploaded and cleared inside the same
-- transaction that inserts the attachment rows, so "did my side effect
-- happen?" is never adjudicated inline: every ambiguous outcome (upload
-- error, unknown commit result, crash between upload and bind) simply leaves
-- the row for the asynchronous reconciler, which settles it long after any
-- in-flight PUT or COMMIT can still land.
--
-- state machine: 'pending' (intent recorded; bind may still claim it) →
-- 'deleting' (a reconciler holds the lease; bind must never attach this key).
-- The reconciler checks for a durable attachment reference only AFTER winning
-- the claim — at that point a bind can no longer succeed on the key, so the
-- check cannot race a late COMMIT.
-- storage_key is the logical primary key, attached in migration 217 via a
-- CONCURRENTLY-built unique index (216) per the repo convention that every
-- migration index — including a new table's unique index — is created
-- concurrently in its own single-statement migration (see 207-209 for the
-- same three-step pattern on client_usage_daily).
CREATE TABLE channel_media_pending_object (
    storage_key      TEXT NOT NULL,
    workspace_id     UUID NOT NULL,
    chat_message_id  UUID NOT NULL,
    storage_url      TEXT NOT NULL,
    -- Ops diagnostics only (per-installation debugging); no logic keys on it.
    installation_id  UUID,
    state            TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'deleting')),
    lease_token      UUID,
    lease_expires_at TIMESTAMPTZ,
    attempt          INT NOT NULL DEFAULT 0,
    next_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error       TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
