CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS global_agent_owner_name_uidx
    ON global_agent (owner_id, name);
