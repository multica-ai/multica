-- Squad names need not be unique within a workspace — different teams may
-- legitimately want the same label (e.g. "Research"), and the leader/UUID
-- already disambiguates them.
ALTER TABLE squad DROP CONSTRAINT IF EXISTS squad_workspace_id_name_key;
