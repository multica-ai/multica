ALTER TABLE autopilot_trigger ADD COLUMN webhook_payload_policy text NOT NULL DEFAULT 'redacted' CHECK (webhook_payload_policy IN ('redacted', 'metadata_only'));
