-- Custom issue property actor types (MUL-6286).
--
-- Adds 'actor' and 'multi_actor' to the type allowlist. Both store a prefixed
-- reference string ("member:<user_id>"); multi_actor stores an array of them.
-- The kind prefix is what lets the accepted-kind set widen later without
-- another migration or a new property type.
--
-- Keeping the value a plain string (rather than an object) means the existing
-- @> containment filter, the jsonb_path_ops GIN index, and the client value
-- schema all keep working unchanged.
--
-- The type is validated again in the handler, which additionally resolves each
-- reference against the workspace. This constraint is only the outer guard.
--
-- NOT VALID + VALIDATE keeps the ACCESS EXCLUSIVE lock instantaneous; existing
-- rows all carry one of the original seven types, so validation is a formality.
ALTER TABLE issue_property DROP CONSTRAINT IF EXISTS issue_property_type_check;
ALTER TABLE issue_property ADD CONSTRAINT issue_property_type_check
    CHECK (type IN ('text', 'number', 'select', 'multi_select', 'date', 'checkbox', 'url', 'actor', 'multi_actor')) NOT VALID;
ALTER TABLE issue_property VALIDATE CONSTRAINT issue_property_type_check;
