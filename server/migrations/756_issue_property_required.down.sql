ALTER TABLE issue_property
    DROP COLUMN IF EXISTS always_visible,
    DROP COLUMN IF EXISTS required;
