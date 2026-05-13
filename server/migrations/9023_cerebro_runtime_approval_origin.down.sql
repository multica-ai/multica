-- JEH-1152: revert origin_type allowlist back to the upstream pair. Any
-- existing 'runtime_approval' rows must be cleared first or the new CHECK
-- will reject the migration.
DELETE FROM issue WHERE origin_type = 'runtime_approval';
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_origin_type_check;
ALTER TABLE issue ADD CONSTRAINT issue_origin_type_check
    CHECK (origin_type IN ('autopilot', 'quick_create'));
