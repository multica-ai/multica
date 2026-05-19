-- 9034_cerebro_runtime_tool_unique_nulls_fix: dedupe cloud rows where
-- mcp_server_name IS NULL.
--
-- JEH-1710 bid 6. Migration 9032 created the unique constraint
--
--   UNIQUE (runtime_id, tool_name, mcp_server_name)
--
-- which Postgres treats with the default NULLS DISTINCT semantics: two rows
-- whose mcp_server_name is NULL never conflict. That is fine for MCP tools
-- (mcp_server_name is always set) but breaks cloud tools, which carry
-- mcp_server_name = NULL. The bid 6 backfill seeder relies on
-- UpsertCerebroRuntimeTool's ON CONFLICT clause to be idempotent across
-- restarts; with NULLS DISTINCT every seeder run would duplicate every
-- cloud row.
--
-- Postgres 15+ supports `UNIQUE NULLS NOT DISTINCT`, which is exactly what
-- we want: NULL is treated as equal to NULL for conflict detection.
--
-- Down: revert to the original constraint shape (so a rollback re-creates
-- the row identical to migration 9032). Down is best-effort: it cannot
-- recover rows that were added between the up and down, but the table is
-- still functional.

ALTER TABLE cerebro_runtime_tool
    DROP CONSTRAINT IF EXISTS cerebro_runtime_tool_unique;

ALTER TABLE cerebro_runtime_tool
    ADD CONSTRAINT cerebro_runtime_tool_unique
    UNIQUE NULLS NOT DISTINCT (runtime_id, tool_name, mcp_server_name);
