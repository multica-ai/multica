-- Per-channel cursor for the outbound reconciler.
--
-- The reconciler compensates for replies the realtime producer never
-- enqueued (the producing replica crashed between finishing the task and
-- inserting the queue row). It re-scans a trailing time window of terminal
-- agent tasks and enqueues anything missing, so it needs somewhere durable to
-- remember how far it has scanned.
--
-- channel_type is the primary key: one cursor per channel, so a slow or
-- wedged scan on one platform cannot stall another's. lease_token +
-- lease_expires_at make the scan single-writer across replicas without a
-- separate lock table — a replica claims the cursor, scans its window, then
-- advances or releases.
--
-- created_at is the queue's own epoch for this channel. The reconciler decides
-- "already delivered" by the absence of a channel_outbound_queue row, so any
-- window reaching back before this row existed classifies every reply the
-- pre-queue path delivered as missing and re-sends it. Flooring the scan at
-- created_at makes that impossible by construction rather than by choosing a
-- lucky seed, and it has to be a column because the cursor advances past its
-- own creation on the first sweep.
CREATE TABLE channel_outbound_reconcile_state (
    channel_type       TEXT PRIMARY KEY,
    cursor_at          TIMESTAMPTZ NOT NULL,
    lease_token        TEXT,
    lease_expires_at   TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
