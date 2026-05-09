-- Reverse of 081_ship_hub_phase_3.up.sql in dependency order.
ALTER TABLE workspace DROP COLUMN IF EXISTS ship_hub_smoke_workflow;
DROP INDEX IF EXISTS idx_ship_card_action_workspace;
DROP INDEX IF EXISTS idx_ship_card_action_pr;
DROP TABLE IF EXISTS ship_card_action;
