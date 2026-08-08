-- Validate the widened CHECK without holding the ACCESS EXCLUSIVE lock from
-- migration 265 for the duration of the scan.
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
