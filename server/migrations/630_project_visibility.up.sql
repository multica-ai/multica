-- MUL-8472: private projects. 'workspace' keeps today's behavior (every
-- workspace member sees the project and its issues); 'private' limits it to
-- project_member rows plus workspace owners/admins.
--
-- The constant default makes this a catalog-only change. Values are validated
-- in code (handler.validProjectVisibilities); there is no CHECK constraint so
-- the ALTER does not scan the table, the same trade-off as inbox_item.section.
ALTER TABLE project ADD COLUMN IF NOT EXISTS visibility TEXT NOT NULL DEFAULT 'workspace';
