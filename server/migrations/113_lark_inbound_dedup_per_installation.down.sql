-- Revert to message_id-only PK. The down migration drops the table and
-- recreates it without the installation_id column; transient cache rows
-- are dropped.
DROP TABLE IF EXISTS lark_inbound_message_dedup;

CREATE TABLE lark_inbound_message_dedup (
    message_id    TEXT PRIMARY KEY,
    received_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at  TIMESTAMPTZ,
    claim_token   UUID NOT NULL DEFAULT gen_random_uuid()
);

CREATE INDEX idx_lark_inbound_dedup_received
    ON lark_inbound_message_dedup(received_at);
