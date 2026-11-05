DROP TABLE IF EXISTS code_graph_build;
ALTER TABLE code_graph_repository DROP COLUMN IF EXISTS active_build_id, DROP COLUMN IF EXISTS public_read_enabled, DROP COLUMN IF EXISTS symbol_count, DROP COLUMN IF EXISTS edge_count;
