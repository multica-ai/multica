-- Validate the widened CHECK separately from migration 643 so the validation
-- scan does not inherit migration 643's ACCESS EXCLUSIVE lock.
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
