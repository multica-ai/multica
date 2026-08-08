-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
--
-- The only hot read: one person's inbox, active rows, newest surfaced first.
-- Column order follows the query — equality on (workspace_id, recipient_id),
-- then archived_at to separate the active view from the archived one, then the
-- sort key. Both sort columns are DESC so the (surfaced_at, id) keyset cursor
-- resolves inside the index instead of leaving the tie-break to a sort step.
CREATE INDEX CONCURRENTLY IF NOT EXISTS inbox_group_recipient_surfaced_idx
    ON inbox_group (workspace_id, recipient_id, archived_at, surfaced_at DESC, id DESC);
