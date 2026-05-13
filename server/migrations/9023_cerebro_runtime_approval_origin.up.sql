-- JEH-1152: allow 'runtime_approval' as an issue.origin_type so the cerebro
-- runtime-access gate can stamp auto-created approval issues with the runtime
-- UUID in origin_id and dedupe future denials against the same runtime.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create', 'runtime_approval'));
