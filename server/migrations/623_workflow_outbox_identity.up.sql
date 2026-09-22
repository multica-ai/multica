CREATE UNIQUE INDEX CONCURRENTLY workflow_outbox_identity_idx ON workflow_outbox (event_id, channel, recipient_id);
