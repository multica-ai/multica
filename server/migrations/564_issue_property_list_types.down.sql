ALTER TABLE issue_property DROP CONSTRAINT IF EXISTS issue_property_type_check;
ALTER TABLE issue_property ADD CONSTRAINT issue_property_type_check
    CHECK (type IN ('text', 'number', 'select', 'multi_select', 'date', 'checkbox', 'url', 'actor', 'multi_actor')) NOT VALID;
ALTER TABLE issue_property VALIDATE CONSTRAINT issue_property_type_check;
