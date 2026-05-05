-- CEREBRO-PATCH(migration-idempotent-058-budget-caps): cerebro modification of upstream file
DROP TABLE IF EXISTS budget_change_log;
DROP TABLE IF EXISTS budget_state;
DROP TABLE IF EXISTS agent_budget_override;
DROP TABLE IF EXISTS user_budget_override;
DROP TABLE IF EXISTS workspace_budget_config;
