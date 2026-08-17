-- Restore migration 190's transitional constraint without changing data. Keep
-- the validated strict constraint under its shadow name until the transitional
-- constraint is installed, so rollback never leaves the table unprotected.
ALTER TABLE agent_task_queue
    RENAME CONSTRAINT agent_task_queue_accountable_matches_originator
    TO agent_task_queue_accountable_matches_originator_strict;

ALTER TABLE agent_task_queue
    ADD CONSTRAINT agent_task_queue_accountable_matches_originator
    CHECK (
        originator_source IS NULL
        OR originator_user_id IS NULL
        OR (
            accountable_user_id IS NOT NULL
            AND accountable_user_id = originator_user_id
        )
    )
    NOT VALID;
