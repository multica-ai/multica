-- Add parent_project_id to support parent/child project relationships
-- (RFC #3104). Nullable so existing projects remain top-level. No foreign
-- key per the repository rule: relationships and cleanup are enforced in the
-- application layer. The index lives in a separate CONCURRENTLY migration.
ALTER TABLE project ADD COLUMN parent_project_id UUID;
