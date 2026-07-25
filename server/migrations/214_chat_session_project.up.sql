-- Chat sessions may opt into one project's durable context. Keep this as a
-- soft reference: adding a foreign key would validate the established,
-- write-active chat_session table and take a cross-table lock during deploy.
-- Create/delete handlers serialize on the project row, project deletion clears
-- existing references, and daemon claim revalidates workspace ownership before
-- injecting any context.
--
-- IF NOT EXISTS because this migration was first shipped as 213_chat_session_project
-- and later renumbered to 214 (#5868, dodging a prefix collision). Environments
-- that already applied it under the old 213 name have the column but not the 214
-- version row, so the runner re-applies it under the new name; without the guard
-- that re-run fails with "column already exists" (SQLSTATE 42701) and blocks deploy.
ALTER TABLE chat_session
  ADD COLUMN IF NOT EXISTS project_id UUID;
