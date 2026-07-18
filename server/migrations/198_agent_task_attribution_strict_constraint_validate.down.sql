-- PostgreSQL cannot mark a validated constraint NOT VALID again. Recreate the
-- shadow constraint to restore the state immediately after migration 197.
ALTER TABLE agent_task_queue
    DROP CONSTRAINT IF EXISTS agent_task_queue_accountable_matches_originator_strict;

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
