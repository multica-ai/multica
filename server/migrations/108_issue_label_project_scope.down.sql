DROP INDEX IF EXISTS issue_label_workspace_name_lower_lookup_idx;
DROP INDEX IF EXISTS issue_label_workspace_project_name_lower_uidx;
DROP INDEX IF EXISTS issue_label_workspace_global_name_lower_uidx;

-- If a deployment rolls back after creating same-name labels in different
-- projects, keep all rows by making duplicates unique before restoring the old
-- workspace-wide uniqueness rule.
UPDATE issue_label AS il
SET name = il.name || ' (' || substring(il.id::text, 1, 8) || ')'
WHERE EXISTS (
    SELECT 1 FROM issue_label il2
    WHERE il2.workspace_id = il.workspace_id
      AND LOWER(il2.name) = LOWER(il.name)
      AND il2.id < il.id
);

ALTER TABLE issue_label
    DROP CONSTRAINT IF EXISTS issue_label_workspace_project_fk,
    DROP COLUMN IF EXISTS project_id;

CREATE UNIQUE INDEX IF NOT EXISTS issue_label_workspace_name_lower_idx
    ON issue_label (workspace_id, LOWER(name));

DROP INDEX IF EXISTS project_workspace_id_id_uidx;
