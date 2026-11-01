-- The squad an agent is drawn in on the office: its home squad. Must be a squad
-- the agent is a member of (enforced in the handler, never a foreign key). NULL
-- means the agent is unassigned and is not drawn. A nullable column with no
-- default is metadata-only on Postgres, so existing agents need no rewrite.
ALTER TABLE agent ADD COLUMN home_squad_id UUID;
