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
-- No created_at: the row is created lazily by the claim query's upsert and
-- lives for the deployment's lifetime, so its insert time carries no
-- information worth a column.
CREATE TABLE channel_outbound_reconcile_state (
    channel_type       TEXT PRIMARY KEY,
    cursor_at          TIMESTAMPTZ NOT NULL,
    lease_token        TEXT,
    lease_expires_at   TIMESTAMPTZ,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
