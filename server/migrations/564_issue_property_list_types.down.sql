-- Restore the pre-list type allowlist.
--
-- Fails closed when any multi_text / multi_url definition still exists, the
-- same stance as 341's down: rewriting or deleting those rows would destroy
-- user data keyed by definition id. Archived definitions count too; their
-- values stay resolvable. Convert the definitions (and the issue values keyed
-- to them) to a pre-list type before rolling back.
DO $$
DECLARE
    list_defs BIGINT;
BEGIN
    SELECT count(*) INTO list_defs
    FROM issue_property
    WHERE type IN ('multi_text', 'multi_url');

    IF list_defs > 0 THEN
        RAISE EXCEPTION 'cannot roll back 564: % multi_text/multi_url property definition(s) still exist', list_defs
            USING HINT = 'Convert those definitions and the issue values keyed to them to a pre-list type first; this migration will not delete user data.';
    END IF;
END
$$;

ALTER TABLE issue_property DROP CONSTRAINT IF EXISTS issue_property_type_check;
ALTER TABLE issue_property ADD CONSTRAINT issue_property_type_check
    CHECK (type IN ('text', 'number', 'select', 'multi_select', 'date', 'checkbox', 'url', 'actor', 'multi_actor')) NOT VALID;
ALTER TABLE issue_property VALIDATE CONSTRAINT issue_property_type_check;
