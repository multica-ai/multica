-- FIR-2442 / MUL-2442 cleanup — one-shot.
--
-- Tine - QA Engineer (agent 8bfccab3-89ea-40cc-b57d-c7eabaa30f50, runtime
-- 19919703-62d1-460e-8d6d-0904dc9a621c) auto-paused at 2026-05-28 08:56 CEST
-- after hitting the Codex usage limit. Between the pause and the deploy of the
-- retry-pause-guard fix, 299 retry rows accumulated on the queue, each marked
-- failed with failure_reason='runtime_paused' immediately after ClaimTask
-- rejected them. No LLM was called and no spend occurred, but the rows clutter
-- the UI and would surface as confusing "old runtime_paused failures" the next
-- time the runtime is unpaused.
--
-- This is a ONE-SHOT cleanup, NOT a migration. Do NOT run it on other
-- environments, other runtimes, or after the next pause episode without
-- review.
--
-- Run with: psql "$DATABASE_URL" -f docs/maintenance/2026-05-28-cleanup-tine-retry-storm.sql
--
-- The script wraps the delete in a transaction and prints the row count
-- before/after so the operator can sanity-check before COMMIT.

BEGIN;

-- Expected ~299. If this returns a wildly different number (e.g. 0 or 5000),
-- ABORT THE TRANSACTION and re-investigate — the situation has changed since
-- 2026-05-28 and the filter may no longer be safe.
SELECT COUNT(*) AS doomed_count
FROM agent_task_queue
WHERE runtime_id      = '19919703-62d1-460e-8d6d-0904dc9a621c'
  AND status          = 'failed'
  AND failure_reason  = 'runtime_paused'
  AND created_at      >= '2026-05-28 08:56:00+02';

DELETE FROM agent_task_queue
WHERE runtime_id      = '19919703-62d1-460e-8d6d-0904dc9a621c'
  AND status          = 'failed'
  AND failure_reason  = 'runtime_paused'
  AND created_at      >= '2026-05-28 08:56:00+02';

-- Sanity check: should print 0. If non-zero, ROLLBACK and investigate.
SELECT COUNT(*) AS remaining_count
FROM agent_task_queue
WHERE runtime_id      = '19919703-62d1-460e-8d6d-0904dc9a621c'
  AND status          = 'failed'
  AND failure_reason  = 'runtime_paused'
  AND created_at      >= '2026-05-28 08:56:00+02';

COMMIT;
