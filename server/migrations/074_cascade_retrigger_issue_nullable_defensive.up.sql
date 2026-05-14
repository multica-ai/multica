-- PUL-102 PR4: drop NOT NULL on cascade_retrigger.issue_id.
--
-- PR4 ships a worker that handles rows with issue_id IS NULL as a
-- "scope_filter_skip" code path. The cascade.Worker.PollOnce flow
-- depends on being able to insert a NULL issue_id row (its
-- TestWorker_NoIssueIDIsScopeSkip test seeds exactly that state).
-- migration 072 (PR1) declared issue_id as NOT NULL because the
-- original design did the [PUL-N] lookup synchronously in the
-- webhook handler; the A6 amendment moved lookup into the worker
-- and the constraint became wrong.
--
-- PR3 has its own migration 073 that does the same drop. This file
-- exists so PR4 can land independently of PR3 — Postgres's
-- ALTER COLUMN ... DROP NOT NULL is idempotent on an already-nullable
-- column, so whichever PR merges first wins and the second PR's
-- migration is a no-op. Once both are merged, the duplicate is
-- harmless dead-DDL we can clean up in a follow-up.

BEGIN;

ALTER TABLE cascade_retrigger
    ALTER COLUMN issue_id DROP NOT NULL;

COMMIT;
