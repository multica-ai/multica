-- Re-add the triggers and tools columns to agent table.
ALTER TABLE agent ADD COLUMN triggers JSONB NOT NULL DEFAULT '[]';
ALTER TABLE agent ADD COLUMN tools JSONB NOT NULL DEFAULT '[]';
