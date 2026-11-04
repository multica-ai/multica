-- Fails while rows of the unretire or unmerge types exist, by design: those
-- applications cannot be reinterpreted under the eight-type list.
ALTER TABLE org_change_application
    DROP CONSTRAINT IF EXISTS org_change_application_change_type_check,
    ADD CONSTRAINT org_change_application_change_type_check CHECK (change_type IN (
        'instruction_change', 'skill_change', 'charter_change', 'autonomy_change',
        'hire', 'retire', 'move', 'merge_squads'));
