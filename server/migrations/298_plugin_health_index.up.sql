CREATE INDEX CONCURRENTLY idx_plugin_health_installation_observed
    ON plugin_health (installation_id, observed_at DESC);
