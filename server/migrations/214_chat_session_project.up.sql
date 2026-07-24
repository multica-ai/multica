-- Chat sessions may opt into one project's durable context. Keep this as a
-- soft reference: adding a foreign key would validate the established,
-- write-active chat_session table and take a cross-table lock during deploy.
-- Create/delete handlers serialize on the project row, project deletion clears
-- existing references, and daemon claim revalidates workspace ownership before
-- injecting any context.
ALTER TABLE chat_session
  ADD COLUMN project_id UUID;
