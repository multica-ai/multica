-- Re-scope lark_inbound_message_dedup to (installation_id, message_id).
--
-- The original schema keyed dedup on `message_id` alone. In a Lark group
-- chat where the same workspace has multiple Multica bots installed, each
-- bot's WS receives the SAME `message_id` from Lark — and both supervisors
-- legitimately need to claim, evaluate AddressedToBot from their own
-- bot's perspective, and either ingest (if @-ed) or drop as
-- not_addressed_in_group (if the OTHER bot was @-ed). With a single
-- shared dedup row, whichever WS claims first locks the row, and the
-- bot that was actually @-ed gets dropped as `duplicate` before it can
-- evaluate the mention. See MUL-2671 multi-bot routing triage.
--
-- The dedup table is short-lived (24h TTL via PurgeLarkInboundDedup);
-- existing rows can be dropped on the way through — there's no
-- meaningful product state in them, only transient idempotency cache.
DROP TABLE IF EXISTS lark_inbound_message_dedup;

CREATE TABLE lark_inbound_message_dedup (
    installation_id  UUID NOT NULL REFERENCES lark_installation(id) ON DELETE CASCADE,
    message_id       TEXT NOT NULL,
    received_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at     TIMESTAMPTZ,
    claim_token      UUID NOT NULL DEFAULT gen_random_uuid(),
    PRIMARY KEY (installation_id, message_id)
);

CREATE INDEX idx_lark_inbound_dedup_received
    ON lark_inbound_message_dedup(received_at);
