-- Adds claim lease columns to agent_task_queue for Phase 2 of the claim
-- reliability fix (MUL-2246 / GitHub #2649).
--
-- claim_token: opaque UUID generated at claim time; the daemon must present
--   it back in StartTask to prove it received the claim response. Prevents
--   a stale daemon from starting a task that was already requeued and
--   re-claimed by another runtime.
--
-- claim_expires_at: absolute deadline by which the daemon must call StartTask
--   with the matching token. If this timestamp passes while the task is still
--   'dispatched', the expired-lease requeue sweep moves it back to 'queued'
--   so it can be re-claimed.

ALTER TABLE agent_task_queue
  ADD COLUMN claim_token UUID,
  ADD COLUMN claim_expires_at TIMESTAMPTZ;
