-- Hook engine: the host calls out to a plugin's own server.
--
-- Everything before this shipped one direction only — a sandboxed surface asks
-- the host, the host acts on the signed-in user's session. This is the reverse
-- edge, so the two new things it needs are a record of what we sent (triage,
-- rate limiting, circuit breaking) and a credential a plugin backend can present
-- when it calls back.

-- One row per hook call. Deliberately NOT an audit log: it is TTL-swept, it
-- stores no request or response body, and nothing in the product reads it to
-- decide what happened to a user's data. Its jobs are operational — show the
-- author why their endpoint is failing, feed the per-hook rate limit, and let
-- the event dispatcher decide when to trip a circuit breaker.
--
-- Bodies are omitted on purpose. A hook payload can carry issue text, and a
-- table that keeps every payload forever is a second copy of workspace content
-- with none of the deletion paths that the first copy has.
CREATE TABLE plugin_invocation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    installation_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    hook_key TEXT NOT NULL CHECK (char_length(hook_key) BETWEEN 1 AND 128),
    trigger TEXT NOT NULL CHECK (trigger IN ('ui', 'manual', 'event', 'agent')),
    status TEXT NOT NULL CHECK (status IN ('ok', 'failed', 'timeout', 'refused')),
    -- Which event caused an event-triggered call. NULL for every other trigger.
    event_type TEXT,
    attempt INTEGER NOT NULL DEFAULT 1 CHECK (attempt BETWEEN 1 AND 10),
    latency_ms INTEGER NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
    -- Redacted, bounded, and only ever the host's own description of the
    -- failure. Never a response body: an endpoint that echoes its input would
    -- otherwise write issue content in here through the back door.
    error TEXT CHECK (error IS NULL OR char_length(error) <= 500),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Install token: lets a plugin's own backend reach the Action API without a
-- user in the loop. Only the hash is stored, so a database read cannot mint a
-- working credential, and rotation is an UPDATE of this column.
--
-- This is the first credential in the system that is not the signed-in user's
-- session. It does not weaken the surface rule — a token still never enters an
-- iframe, and a surface still cannot obtain one — but the claim is now "tokens
-- only move between servers" rather than "there are no tokens".
ALTER TABLE plugin_installation ADD COLUMN IF NOT EXISTS token_hash TEXT;
ALTER TABLE plugin_installation ADD COLUMN IF NOT EXISTS token_rotated_at TIMESTAMPTZ;

-- A fourth comment author. Identity follows the trigger: a ui/manual hook acts
-- for the person who pressed the button and stays attributed to them (with
-- via_plugin_id naming the plugin), while an event hook has no person behind it
-- and must not borrow one. author_id then holds the installation id.
--
-- Same reason as migration 107, which added 'system': a row the platform
-- produced that would be a lie attributed to any member. NOT the same shape,
-- though — 107 revalidated the whole table under ACCESS EXCLUSIVE, which was
-- cheap on the comment table of that era and is not on today's. NOT VALID adds
-- the constraint without a scan; migration 365 validates it under a lock that
-- readers and writers can share.
--
-- New rows are checked from the moment this lands. NOT VALID only means "do not
-- re-check the rows already here", and the widened set is a superset of the old
-- one, so there is nothing here that could violate it.
ALTER TABLE comment DROP CONSTRAINT IF EXISTS comment_author_type_check;
ALTER TABLE comment ADD CONSTRAINT comment_author_type_check
    CHECK (author_type IN ('member', 'agent', 'system', 'plugin')) NOT VALID;
