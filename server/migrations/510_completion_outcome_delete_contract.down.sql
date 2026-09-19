ALTER TABLE agent_task_completion_outcome
    DROP CONSTRAINT agent_task_completion_outcome_final_comment_id_fkey;

ALTER TABLE agent_task_completion_outcome
    ADD CONSTRAINT agent_task_completion_outcome_final_comment_id_fkey
    FOREIGN KEY (final_comment_id) REFERENCES comment(id) ON DELETE SET NULL;
