CREATE UNIQUE INDEX CONCURRENTLY tag_access_connection_close_receipt_delivery_uidx
    ON tag_access_connection_close_receipt (delivery_id, target_digest);
