CREATE UNIQUE INDEX CONCURRENTLY corpus_transfer_ack_primary_uidx ON corpus_transfer_ack (workspace_id, transfer_id, sink_id);
