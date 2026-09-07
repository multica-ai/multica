-- Runtime-level default model connection. A Pi runtime uses this connection
-- when the bound agent has no per-agent runtime_config / API-key override.
-- The secret is intentionally kept in its own column and is never serialized
-- by the ordinary runtime resource.

ALTER TABLE agent_runtime
    ADD COLUMN default_model_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN default_model_api_key TEXT;
