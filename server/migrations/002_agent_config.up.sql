-- Add agent configuration columns: skills, tools, triggers
ALTER TABLE agent
    ADD COLUMN description TEXT NOT NULL DEFAULT '',
    ADD COLUMN skills TEXT NOT NULL DEFAULT '',
    ADD COLUMN tools JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN triggers JSONB NOT NULL DEFAULT '[]';
