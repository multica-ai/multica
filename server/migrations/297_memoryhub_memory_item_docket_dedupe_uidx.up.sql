CREATE UNIQUE INDEX CONCURRENTLY memoryhub_memory_item_docket_dedupe_uidx ON memoryhub_memory_item (docket_id, dedupe_key);
