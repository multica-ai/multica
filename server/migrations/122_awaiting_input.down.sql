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
