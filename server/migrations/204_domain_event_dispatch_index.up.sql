-- Single-statement CONCURRENTLY migration (see 203). Backs the PR3 matcher's
-- claim scan for undispatched, now-available events in seq order:
--   WHERE dispatch_status = 'pending' AND available_at <= now() ORDER BY seq
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_domain_event_dispatch
    ON domain_event (dispatch_status, available_at, seq);
