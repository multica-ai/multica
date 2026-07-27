-- Intent ledger for channel media objects (two-system atomicity, PR #5580).
-- A row is written BEFORE the object is uploaded and cleared inside the same
-- transaction that inserts the attachment rows, so "did my side effect
-- happen?" is never adjudicated inline: every ambiguous outcome (upload
-- error, unknown commit result, crash between upload and bind) simply leaves
-- the row for the asynchronous reconciler, which settles it long after any
-- in-flight PUT or COMMIT can still land.
--
-- state machine: 'pending' (intent recorded; bind may still claim it) →
-- 'deleting' (a reconciler holds the lease; bind must never attach this key) →
-- 'tombstoned' (the object was deleted, but the row is kept so late
-- materializations are re-deleted; see below).
-- The reconciler checks for a durable attachment reference only AFTER winning
-- the claim — at that point a bind can no longer succeed on the key, so the
-- check cannot race a late COMMIT.
--
-- The tombstone exists because a DELETE cannot be ordered against a PUT the
-- client already abandoned: the store may still materialize that object after
-- the delete. Clearing the row immediately would leave such an object with no
-- ledger row and nothing to reclaim it, which would make the settle delay the
-- de-facto correctness barrier for the PUT/DELETE race. Instead the row stays
-- as a tombstone and the object is re-deleted on a widening schedule
-- (attempt-indexed), so a late materialization is caught by a later pass.
-- storage_key is the logical primary key, attached in migration 228 via a
-- CONCURRENTLY-built unique index (227) per the repo convention that every
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
        CHECK (state IN ('pending', 'deleting', 'tombstoned')),
    lease_token      UUID,
    lease_expires_at TIMESTAMPTZ,
    attempt          INT NOT NULL DEFAULT 0,
    next_attempt_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_error       TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
