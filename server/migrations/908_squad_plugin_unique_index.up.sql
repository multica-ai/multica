CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_squad_plugin_squad_name
    ON squad_plugin (squad_id, plugin_name);
