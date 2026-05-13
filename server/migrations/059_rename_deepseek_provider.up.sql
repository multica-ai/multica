-- Rename the provider from "deepseek" to "DeepSeek-TUI" for consistency
-- with the new provider identifier used by the daemon.
UPDATE agent_runtime SET provider = 'DeepSeek-TUI' WHERE provider = 'deepseek';
