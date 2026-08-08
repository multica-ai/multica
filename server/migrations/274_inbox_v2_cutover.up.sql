-- The write gate.
--
-- A table rather than an environment variable or a config flag, because the
-- delivery path reads it INSIDE its own transaction. That removes the window a
-- cached or per-process flag would create: with N instances each holding their
-- own view of the switch, flipping it means a period where some deliveries
-- write groups and others do not, and the rows written during that window are
-- exactly the ones reconcile would have to find later. Reading the row in the
-- same transaction that writes the delivery makes the flip atomic with respect
-- to every delivery, at the cost of one index lookup on a single-row table that
-- is permanently in cache.
--
-- Single row, enforced by a CHECK on a fixed primary key rather than by
-- convention: a second row would silently give different transactions different
-- answers depending on which one they read.
CREATE TABLE IF NOT EXISTS inbox_v2_cutover (
    id            BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    -- Off on deploy. Turning the switch on is a separate, observable act from
    -- shipping the code that can honour it.
    write_enabled BOOLEAN NOT NULL DEFAULT false,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO inbox_v2_cutover (id, write_enabled) VALUES (true, false)
ON CONFLICT (id) DO NOTHING;
