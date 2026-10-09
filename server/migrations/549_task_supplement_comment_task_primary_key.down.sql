-- PostgreSQL drops the attached (comment_id, task_id) index with the constraint;
-- 548 down then restores single-comment uniqueness.
ALTER TABLE task_supplement DROP CONSTRAINT IF EXISTS task_supplement_pkey;
