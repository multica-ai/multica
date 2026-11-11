-- Validate the widened CHECK separately from migration 648 so the validation
-- scan does not inherit migration 648's ACCESS EXCLUSIVE lock.
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
