-- 9105_cerebro_connection_scopable_args: declare per-connection scopable args.
--
-- FIR-2083 Phase 2. A connection can now declare which tool argument is the
-- "scoping axis" — the argument whose value the tool-policy WHEN layer restricts
-- via an argument-value allowlist (see cerebro_tool_policy.conditions.arg_allowlist).
-- For firtal-data-registry this is tool `query_run`, argument `data_source_id`,
-- with options filled from `data_sources_list`.
--
-- scopable_args is a JSON array of objects:
--   [{ "tool": "query_run", "arg": "data_source_id",
--      "options_source_tool": "data_sources_list",
--      "group_by": "folder", "tag_field": "tags", "label": "Data sources" }]
--
-- Default empty array = no scopable arguments = unchanged behavior. Purely
-- additive; no existing connection is affected.

ALTER TABLE workspace_connection
    ADD COLUMN IF NOT EXISTS scopable_args JSONB NOT NULL DEFAULT '[]';
