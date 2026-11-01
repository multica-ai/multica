-- The office room an agent sits in: the department it works for. A constant
-- default is metadata-only on Postgres 11+, so existing agents need no rewrite.
ALTER TABLE agent ADD COLUMN office_room TEXT NOT NULL DEFAULT 'general';
