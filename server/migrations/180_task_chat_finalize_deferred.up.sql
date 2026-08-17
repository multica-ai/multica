-- #5219: cancellation-time chat finalization can race the daemon's transcript
-- flush. When a started chat task is cancelled while its transcript is still
-- empty, the empty/non-empty judgment is deferred until the daemon acks its
-- flush (or a sweeper grace period expires). This column is both the pending
-- marker and the grace-period clock; clearing it doubles as the atomic claim
-- that keeps the ack path and the sweeper from finalizing the same task twice.
--
-- The sweeper index lives in migration 181: agent_task_queue is a hot table,
-- so the index build must run CONCURRENTLY in its own single-statement file.
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS chat_finalize_deferred_at TIMESTAMPTZ;
