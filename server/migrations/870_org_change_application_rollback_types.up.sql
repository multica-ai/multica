-- Rollback types: unretire (restores a retired agent or squad) and unmerge
-- (restores the source of an applied merge). NOT VALID then VALIDATE keeps the
-- ALTER from holding a lock while the table is scanned; the new list is a
-- superset of the old one, so existing rows pass.
ALTER TABLE org_change_application
    DROP CONSTRAINT IF EXISTS org_change_application_change_type_check,
    ADD CONSTRAINT org_change_application_change_type_check CHECK (change_type IN (
        'instruction_change', 'skill_change', 'charter_change', 'autonomy_change',
        'hire', 'retire', 'move', 'merge_squads', 'unretire', 'unmerge')) NOT VALID;
ALTER TABLE org_change_application VALIDATE CONSTRAINT org_change_application_change_type_check;
