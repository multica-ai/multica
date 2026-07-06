-- Phase 3 control plane part 2: per-agent tool allowlist.
-- Restricts which MCP tools an operational agent may call. NULL means no
-- restriction (default, preserves existing behavior). Enforced in execenv
-- when the spawned CLI MCP config is written.
ALTER TABLE agent ADD COLUMN allowed_tools JSONB;
