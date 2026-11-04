-- The squad a project is assigned to. A squad holds at most one project, and the
-- company room shows it. NULL means no squad. No foreign key: the handler checks
-- the squad belongs to the workspace and clears it on the squad's other projects.
ALTER TABLE project ADD COLUMN IF NOT EXISTS squad_id UUID;
