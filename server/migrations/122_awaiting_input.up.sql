-- Add awaiting_input to workflow_node_run status CHECK constraint.
-- The inline CHECK constraint auto-generated name survives the table rename
-- from workflow_node_run → multica_workflow_node_run. Use a DO block to find
-- and drop it safely without hardcoding the constraint name.

DO $$
DECLARE
    cn text;
BEGIN
    SELECT conname INTO cn
    FROM pg_constraint
    WHERE conrelid = 'multica_workflow_node_run'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) ILIKE '%status%';

    IF FOUND THEN
        EXECUTE format('ALTER TABLE multica_workflow_node_run DROP CONSTRAINT %I', cn);
    END IF;

    EXECUTE format('ALTER TABLE multica_workflow_node_run ADD CONSTRAINT %I CHECK (status IN (
        ''pending'',
        ''format_checking'',
        ''format_ok'',
        ''format_failed'',
        ''worker_assigned'',
        ''working'',
        ''awaiting_input'',
        ''awaiting_critic'',
        ''critic_reviewing'',
        ''critic_approved'',
        ''critic_rework'',
        ''completed'',
        ''failed'',
        ''blocked'',
        ''skipped'',
        ''cancelled''
    ))', cn);
END $$;
