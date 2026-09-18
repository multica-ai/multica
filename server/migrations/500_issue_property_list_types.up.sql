-- Custom issue property list types: multi_text / multi_url.
--
-- Adds 'multi_text' and 'multi_url' to the type allowlist. Both store an
-- array of free-form strings (text entries or http(s) URLs) in insertion
-- order; validation, dedup, and the entry cap live in the handler, which is
-- also where each element is validated exactly like a single text / url
-- value. This constraint is only the outer guard.
--
-- Like the actor types (341), the values ride the existing properties jsonb
-- bag: the @> containment filter for element-equality, the jsonb_path_ops GIN
-- index, and the client value schema all keep working unchanged.
--
-- NOT VALID + VALIDATE keeps the ACCESS EXCLUSIVE lock instantaneous;
-- existing rows cannot carry the new types, so validation is a formality.
ALTER TABLE issue_property DROP CONSTRAINT IF EXISTS issue_property_type_check;
ALTER TABLE issue_property ADD CONSTRAINT issue_property_type_check
    CHECK (type IN ('text', 'number', 'select', 'multi_select', 'date', 'checkbox', 'url', 'actor', 'multi_actor', 'multi_text', 'multi_url')) NOT VALID;
ALTER TABLE issue_property VALIDATE CONSTRAINT issue_property_type_check;
