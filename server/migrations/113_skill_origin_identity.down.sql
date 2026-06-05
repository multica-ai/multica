-- Reverse of 113_skill_origin_identity, in reverse order.
--
-- KNOWN ONE-WAY RISK: re-adding UNIQUE(workspace_id, name) can FAIL if, while
-- the origin identity was active, two skills with the same name but different
-- origins were created (which is exactly what the up migration now permits).
-- The name-unique constraint that this restores would reject that data. There
-- is no safe automatic merge here, so this down migration is best-effort and
-- may require manual dedup of colliding names before it can apply.

ALTER TABLE skill DROP CONSTRAINT IF EXISTS skill_workspace_origin_unique;
ALTER TABLE skill ADD CONSTRAINT skill_workspace_id_name_key UNIQUE (workspace_id, name);
ALTER TABLE skill DROP COLUMN IF EXISTS origin;
