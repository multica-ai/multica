-- Agents are drawn in their home squad's room (agent.home_squad_id), so the
-- department label is dead. No release tag ever carried the column.
ALTER TABLE agent DROP COLUMN IF EXISTS office_room;
