-- name: TryRecordInboundEvent :one
-- Insert a fresh dedup row for (provider, event_id). When the row already
-- exists (the platform replayed an event we have already processed),
-- ON CONFLICT DO NOTHING suppresses the insertion AND prevents this
-- statement from emitting any RETURNING row. Callers therefore detect
-- replays via pgx.ErrNoRows on the :one result.
--
-- The STA-8 spec called for "RETURNING xmax" with the convention that
-- xmax==0 means "newly inserted". sqlc 1.31 doesn't expose system
-- columns (xmax) in its column resolver, but the semantic the spec
-- wants is "did this statement create the row?" — and DO NOTHING gives
-- us that for free: a returned row IS the new insertion, no row IS the
-- collision. So we RETURN the user-visible processed_at column purely
-- so sqlc has a non-system column to bind to; the actual signal is
-- presence-vs-absence of a row, which the caller wraps into a
-- bool (inserted) at the dao boundary.
INSERT INTO channel_inbound_event_dedup (provider, connection_id, event_id)
VALUES ($1, $2, $3)
ON CONFLICT (connection_id, event_id) DO NOTHING
RETURNING processed_at;

-- name: CleanupOldInboundEventDedup :exec
DELETE FROM channel_inbound_event_dedup
WHERE processed_at < now() - interval '7 days';
