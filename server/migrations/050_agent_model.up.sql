-- Adds an explicit per-agent model field. Previously the only way to
-- pick a model per agent was via custom_env / custom_args; a first-class
-- column lets the UI render a dropdown and keeps Codex-style app-server
-- providers (which reject -m in custom_args) working without CLI flags.
ALTER TABLE agent ADD COLUMN model TEXT;
