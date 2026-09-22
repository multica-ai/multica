CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_entity_alias_lookup_uidx ON knowledge_entity_alias (knowledge_base_id, normalized_alias, disambiguator);
