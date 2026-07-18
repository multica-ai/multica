-- Human Attribution rollout, phase 2: install the strict one-way invariant
-- alongside migration 190's transitional constraint (MUL-4302).
--
-- Migration 190 temporarily exempted rows with originator_source IS NULL so
-- active legacy tasks and stale writers could survive the rolling deployment.
-- Those rows have now been backfilled out of band. Install the strict form
-- under a temporary name first so it starts protecting new writes immediately
-- while the existing constraint remains in place.
--
-- Keep this ADD separate from migration 198's full-table VALIDATE. PostgreSQL
-- holds the ACCESS EXCLUSIVE lock taken here until the statement transaction
-- ends; combining both steps would unnecessarily block queue traffic for the
-- duration of the validation scan.
ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_accountable_matches_originator_strict
    CHECK (
        originator_user_id IS NULL
        OR (
            accountable_user_id IS NOT NULL
            AND accountable_user_id = originator_user_id
        )
    )
    NOT VALID;
