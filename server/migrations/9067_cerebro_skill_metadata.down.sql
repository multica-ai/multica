DROP INDEX IF EXISTS idx_skill_metadata_status;
DROP INDEX IF EXISTS idx_skill_metadata_category;
DROP INDEX IF EXISTS idx_skill_metadata_domain;
DROP INDEX IF EXISTS idx_skill_metadata_gin;
ALTER TABLE skill DROP COLUMN IF EXISTS metadata;
