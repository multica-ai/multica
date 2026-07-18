-- The strict shadow constraint is validated, so migration 190's transitional
-- originator_source IS NULL exemption can now be removed. Both statements are
-- sent in one migration query and therefore commit atomically: readers never
-- observe the table without either constraint in place.
ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_accountable_matches_originator;

ALTER TABLE agent_task_queue
    RENAME CONSTRAINT agent_task_queue_accountable_matches_originator_strict
    TO agent_task_queue_accountable_matches_originator;
