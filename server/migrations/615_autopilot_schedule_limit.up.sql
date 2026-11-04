DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM schema_migrations WHERE version = '600_autopilot_schedule_limit') THEN
        RAISE EXCEPTION '600_autopilot_schedule_limit is already applied; refusing to reapply the renamed migration'
            USING HINT = 'See server/migrations/README.md: Limited schedule migration rename. Preserve execution counts before upgrading.';
    END IF;
END $$;

ALTER TABLE autopilot_trigger
    ADD COLUMN execution_limit integer,
    ADD COLUMN execution_count bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT autopilot_trigger_execution_limit_check CHECK (
        execution_limit IS NULL OR (kind = 'schedule' AND execution_limit > 0)
    ),
    ADD CONSTRAINT autopilot_trigger_execution_count_check CHECK (execution_count >= 0);
