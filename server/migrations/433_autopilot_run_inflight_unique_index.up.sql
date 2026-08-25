-- ALL-211 P0 (BLOCKING 3): DB-level hard mutual exclusion for autopilot
-- dispatch — at most ONE in-flight run per autopilot, enforced by the
-- database instead of a racy check-then-insert in the application.
--
-- "In-flight" is exactly the state set aligned in ALL-211 BLOCKING 2:
-- status IN ('issue_created', 'running'), matching the CHECK constraint
-- autopilot_run_status_check and the partial index idx_autopilot_run_status.
-- A concurrent dispatch that loses the race fails its INSERT with error
-- code 23505 on this index, which the dispatch path maps to a `skipped` run
-- with reason already_active (the same pattern as
-- recoverConcurrentWebhookAdmission).
--
-- CONCURRENTLY because autopilot_run is a hot table (the scheduler writes
-- a row per slot) and a plain CREATE INDEX would take an ACCESS EXCLUSIVE
-- lock that stalls dispatch. Matches the 035/067/074/075/078/080 convention.
-- The migration runner cannot mix CONCURRENTLY with other statements in the
-- same file, so this file is a single statement; the index comment lives in
-- migration 434.
--
-- IF NOT EXISTS (257/261 convention) pairs with the registered cleanup hook
-- in cmd/migrate/main.go (concurrentIndexCleanups): an interrupted build
-- leaves an INVALID uq_autopilot_run_inflight behind, the hook drops it
-- before the retry, and IF NOT EXISTS then rebuilds instead of wedging on
-- "already exists" — otherwise an interrupted build could be recorded as
-- applied while the mutual-exclusion guarantee is silently INVALID, which
-- is exactly the duplicate-execution risk ALL-234 defect 2 closes.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_autopilot_run_inflight
ON autopilot_run (autopilot_id)
WHERE status IN ('issue_created', 'running');
