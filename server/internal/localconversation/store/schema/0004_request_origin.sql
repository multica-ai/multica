-- Which device asked for each turn. Empty when this device did, or when no device did.
ALTER TABLE turn ADD COLUMN origin_peer_id TEXT NOT NULL DEFAULT '';
ALTER TABLE turn ADD COLUMN origin_client_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE prompt_queue_item ADD COLUMN origin_peer_id TEXT NOT NULL DEFAULT '';
ALTER TABLE prompt_queue_item ADD COLUMN origin_client_kind TEXT NOT NULL DEFAULT '';
