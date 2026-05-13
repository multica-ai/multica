-- PUL-102 PR3: allow cascade_retrigger.issue_id to be NULL on initial insert.
--
-- The original migration 072 declared issue_id as NOT NULL because the
-- naive design did the [PUL-N] lookup in the webhook handler. The A6
-- amendment in the plan moved heavy lifting (lookup, state validation,
-- spawn) into the background queue worker (PR4), so the handler now
-- persists events immediately on receipt and the worker fills issue_id
-- later by parsing PR title / branch.
--
-- Trade-off accepted: rows with issue_id IS NULL until the worker
-- processes them. The unprocessed-FIFO index from 072 already filters
-- on processed_at IS NULL so query plans are unaffected.
--
-- Pre-existing rows: none. cascade_retrigger only began receiving
-- writes in PR3 (after this migration), so the NULL drop is safe with
-- no backfill required.

BEGIN;

ALTER TABLE cascade_retrigger
    ALTER COLUMN issue_id DROP NOT NULL;

COMMIT;
