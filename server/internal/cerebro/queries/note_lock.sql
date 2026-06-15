-- Wave 3 / G3 (TECH-3556) — interim edit lock on a Note.
-- See migration 9079_cerebro_note_lock for the heartbeat + timeout model.
-- The "is the lock live?" decision is taken in the handler by comparing
-- lock_heartbeat_at against now()-timeout; these queries just read and mutate
-- the three lock columns on cerebro_note.

-- name: GetNoteLock :one
SELECT artifact_id, locked_by, locked_at, lock_heartbeat_at
FROM cerebro_note
WHERE artifact_id = $1;

-- name: AcquireNoteLock :one
-- Acquire (or refresh, or re-acquire) the lock for $2 (the caller). Succeeds —
-- returns a row — only when the lock is free, already held by the caller, or
-- stale (heartbeat older than the cutoff $3). On a fresh acquire locked_at is
-- set; on re-acquire by the same holder it is preserved. Returns no rows when a
-- different user holds a LIVE lock (the caller must then take over or wait).
UPDATE cerebro_note
SET locked_by         = $2,
    locked_at         = CASE WHEN locked_by = $2 THEN COALESCE(locked_at, now()) ELSE now() END,
    lock_heartbeat_at = now(),
    updated_at        = now()
WHERE artifact_id = $1
  AND (locked_by IS NULL OR locked_by = $2 OR lock_heartbeat_at IS NULL OR lock_heartbeat_at < $3)
RETURNING artifact_id, locked_by, locked_at, lock_heartbeat_at;

-- name: ForceAcquireNoteLock :one
-- Take over: steal the lock unconditionally for $2. Used by the "Take over"
-- action when a live lock is held by someone else.
UPDATE cerebro_note
SET locked_by         = $2,
    locked_at         = now(),
    lock_heartbeat_at = now(),
    updated_at        = now()
WHERE artifact_id = $1
RETURNING artifact_id, locked_by, locked_at, lock_heartbeat_at;

-- name: HeartbeatNoteLock :one
-- Keep the lock alive. Only the current holder may heartbeat; returns no rows
-- if the caller no longer holds it (e.g. it was taken over), so the client
-- knows to drop to read-only.
UPDATE cerebro_note
SET lock_heartbeat_at = now(),
    updated_at        = now()
WHERE artifact_id = $1 AND locked_by = $2
RETURNING artifact_id, locked_by, locked_at, lock_heartbeat_at;

-- name: ReleaseNoteLock :exec
-- Release the lock, but only if the caller holds it (a stale takeover must not
-- be cleared by the previous holder's late "close").
UPDATE cerebro_note
SET locked_by         = NULL,
    locked_at         = NULL,
    lock_heartbeat_at = NULL,
    updated_at        = now()
WHERE artifact_id = $1 AND locked_by = $2;
